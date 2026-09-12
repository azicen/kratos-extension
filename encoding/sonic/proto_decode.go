//go:build go1.27

package sonic

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"uuid"

	jsonsonic "github.com/bytedance/sonic"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// unmarshalProto3 将已解析的 JSON 节点写入 Proto3 反射消息
func unmarshalProto3(node *jsonNode, message protoreflect.Message, options Options, jsonAPI jsonsonic.API, metadata *descriptorMetadataCache) error {
	decoder := protoDecoder{options: options, json: jsonAPI, metadata: metadata}
	return decoder.readMessage(node, message)
}

// protoDecoder 保存 Proto3 解码配置和当前消息递归深度
type protoDecoder struct {
	// options 控制未知字段、Any Resolver 和递归限制
	options Options
	// json 提供 Descriptor 字段名称的固定 JSON 编码能力
	json jsonsonic.API
	// metadata 缓存 Descriptor 派生的字段查找和索引信息
	metadata *descriptorMetadataCache
	// depth 记录当前嵌套消息深度
	depth int
}

// readMessage 根据消息完整名称分派普通消息、UUID 和 Well-Known Types
func (d *protoDecoder) readMessage(node *jsonNode, message protoreflect.Message) error {
	if err := d.enter(); err != nil {
		return err
	}
	defer d.leave()

	switch message.Descriptor().FullName() {
	case uuidMessageName:
		return d.readUUID(node, message)
	case "google.protobuf.Any":
		return d.readAny(node, message)
	case "google.protobuf.Timestamp":
		return d.readTimestamp(node, message)
	case "google.protobuf.Duration":
		return d.readDuration(node, message)
	case "google.protobuf.BoolValue", "google.protobuf.Int32Value", "google.protobuf.Int64Value",
		"google.protobuf.UInt32Value", "google.protobuf.UInt64Value", "google.protobuf.FloatValue",
		"google.protobuf.DoubleValue", "google.protobuf.StringValue", "google.protobuf.BytesValue":
		field := message.Descriptor().Fields().ByNumber(1)
		value, valid, err := d.readScalar(node, field)
		if err != nil {
			return err
		}
		if valid {
			message.Set(field, value)
		}
		return nil
	case "google.protobuf.Empty":
		if node.kind != nodeObject {
			return errors.New("sonic: google.protobuf.Empty must be an object")
		}
		if len(node.object) != 0 && !d.options.DiscardUnknown {
			return fmt.Errorf("sonic: unknown field %q", node.object[0].name)
		}
		return nil
	case "google.protobuf.Struct":
		return d.readStruct(node, message)
	case "google.protobuf.ListValue":
		return d.readListValue(node, message)
	case "google.protobuf.Value":
		return d.readKnownValue(node, message)
	case "google.protobuf.FieldMask":
		return d.readFieldMask(node, message)
	}
	return d.readOrdinaryMessage(node, message)
}

// readOrdinaryMessage 匹配 JSON 名称或 Proto 原始名称，并写入普通 Proto3 消息
func (d *protoDecoder) readOrdinaryMessage(node *jsonNode, message protoreflect.Message) error {
	if node.kind != nodeObject {
		return fmt.Errorf("sonic: message %s must be a JSON object", message.Descriptor().FullName())
	}
	metadata := d.metadata.message(message.Descriptor())
	seenFields := newIndexSet(len(metadata.fields))
	seenOneofs := newIndexSet(metadata.oneofCount)
	for _, member := range node.object {
		index, exists := metadata.byName[member.name]
		if !exists {
			if d.options.DiscardUnknown {
				continue
			}
			return fmt.Errorf("sonic: unknown field %q for %s", member.name, message.Descriptor().FullName())
		}
		entry := &metadata.fields[index]
		field := entry.field
		if !seenFields.add(index) {
			return fmt.Errorf("sonic: duplicate field %q", member.name)
		}
		if member.value.kind == nodeNull && !isKnownValueField(field) && !isNullValueField(field) {
			continue
		}
		if entry.oneofIndex >= 0 {
			if !seenOneofs.add(entry.oneofIndex) {
				oneof := field.ContainingOneof()
				return fmt.Errorf("sonic: oneof %s is already set", oneof.FullName())
			}
		}
		if err := d.readField(member.value, message, field); err != nil {
			return fmt.Errorf("sonic: field %s: %w", field.FullName(), err)
		}
	}
	return nil
}

// indexSet 使用单个 uint64 保存小集合，较大集合才分配额外位图
type indexSet struct {
	inline uint64
	extra  []uint64
}

// newIndexSet 按元素数量创建位集；64 个以内不发生堆分配
func newIndexSet(size int) indexSet {
	if size <= 64 {
		return indexSet{}
	}
	return indexSet{extra: make([]uint64, (size+63)/64)}
}

// add 记录索引并返回它此前是否尚未出现
func (s *indexSet) add(index int) bool {
	word := index / 64
	mask := uint64(1) << (index % 64)
	if len(s.extra) == 0 {
		if s.inline&mask != 0 {
			return false
		}
		s.inline |= mask
		return true
	}
	if s.extra[word]&mask != 0 {
		return false
	}
	s.extra[word] |= mask
	return true
}

// readField 根据字段基数和 kind 分派单值、列表、Map 或嵌套消息解码
func (d *protoDecoder) readField(node *jsonNode, message protoreflect.Message, field protoreflect.FieldDescriptor) error {
	if field.IsList() {
		return d.readList(node, message.Mutable(field).List(), field)
	}
	if field.IsMap() {
		return d.readMap(node, message.Mutable(field).Map(), field)
	}
	if field.Kind() == protoreflect.MessageKind {
		value := message.NewField(field)
		if err := d.readMessage(node, value.Message()); err != nil {
			return err
		}
		message.Set(field, value)
		return nil
	}
	value, valid, err := d.readScalar(node, field)
	if err != nil {
		return err
	}
	if valid {
		message.Set(field, value)
	}
	return nil
}

// readList 解码 repeated 字段，并拒绝非 Value 类型的 null 元素
func (d *protoDecoder) readList(node *jsonNode, list protoreflect.List, field protoreflect.FieldDescriptor) error {
	if node.kind != nodeArray {
		return errors.New("repeated field must be a JSON array")
	}
	for index, item := range node.array {
		if item.kind == nodeNull && !isKnownValueField(field) {
			return fmt.Errorf("null is not valid at index %d", index)
		}
		if field.Kind() == protoreflect.MessageKind {
			value := list.NewElement()
			if err := d.readMessage(item, value.Message()); err != nil {
				return err
			}
			list.Append(value)
			continue
		}
		value, valid, err := d.readScalar(item, field)
		if err != nil {
			return err
		}
		if valid {
			list.Append(value)
		}
	}
	return nil
}

// readMap 解码 Map 键和值，并检测规范化后的重复 Map 键
func (d *protoDecoder) readMap(node *jsonNode, mapping protoreflect.Map, field protoreflect.FieldDescriptor) error {
	if node.kind != nodeObject {
		return errors.New("map field must be a JSON object")
	}
	for _, member := range node.object {
		key, err := parseMapKey(member.name, field.MapKey())
		if err != nil {
			return err
		}
		if mapping.Has(key) {
			return fmt.Errorf("duplicate map key %q", member.name)
		}
		if field.MapValue().Kind() == protoreflect.MessageKind {
			value := mapping.NewValue()
			if err := d.readMessage(member.value, value.Message()); err != nil {
				return err
			}
			mapping.Set(key, value)
			continue
		}
		value, valid, err := d.readScalar(member.value, field.MapValue())
		if err != nil {
			return err
		}
		if valid {
			mapping.Set(key, value)
		}
	}
	return nil
}

// readScalar 按字段 kind 校验并解析单个 JSON 标量
func (d *protoDecoder) readScalar(node *jsonNode, field protoreflect.FieldDescriptor) (protoreflect.Value, bool, error) {
	switch field.Kind() {
	case protoreflect.BoolKind:
		if node.kind == nodeBool {
			return protoreflect.ValueOfBool(node.boolean), true, nil
		}
	case protoreflect.StringKind:
		if node.kind == nodeString {
			return protoreflect.ValueOfString(node.text), true, nil
		}
	case protoreflect.BytesKind:
		if node.kind == nodeString {
			value, err := decodeBase64(node.text)
			return protoreflect.ValueOfBytes(value), err == nil, err
		}
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		value, err := parseSigned(node, 32)
		return protoreflect.ValueOfInt32(int32(value)), err == nil, err
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		value, err := parseSigned(node, 64)
		return protoreflect.ValueOfInt64(value), err == nil, err
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		value, err := parseUnsigned(node, 32)
		return protoreflect.ValueOfUint32(uint32(value)), err == nil, err
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		value, err := parseUnsigned(node, 64)
		return protoreflect.ValueOfUint64(value), err == nil, err
	case protoreflect.FloatKind:
		value, err := parseFloat(node, 32)
		return protoreflect.ValueOfFloat32(float32(value)), err == nil, err
	case protoreflect.DoubleKind:
		value, err := parseFloat(node, 64)
		return protoreflect.ValueOfFloat64(value), err == nil, err
	case protoreflect.EnumKind:
		if node.kind == nodeNull && isNullValueField(field) {
			return protoreflect.ValueOfEnum(0), true, nil
		}
		if node.kind == nodeString {
			value := field.Enum().Values().ByName(protoreflect.Name(node.text))
			if value != nil {
				return protoreflect.ValueOfEnum(value.Number()), true, nil
			}
			if d.options.DiscardUnknown {
				return protoreflect.Value{}, false, nil
			}
		}
		if node.kind == nodeNumber {
			value, err := strconv.ParseInt(node.text, 10, 32)
			if err == nil {
				return protoreflect.ValueOfEnum(protoreflect.EnumNumber(value)), true, nil
			}
		}
	}
	return protoreflect.Value{}, false, fmt.Errorf("invalid %s value", field.Kind())
}

// readUUID 将规范 UUID 字符串直接解析为底层 16 字节字段
func (d *protoDecoder) readUUID(node *jsonNode, message protoreflect.Message) error {
	if node.kind != nodeString {
		return errors.New("UUID must be a JSON string")
	}
	field := message.Descriptor().Fields().ByNumber(1)
	if node.text == "" {
		message.Set(field, protoreflect.ValueOfBytes(nil))
		return nil
	}
	value, err := uuid.Parse(node.text)
	if err != nil {
		return err
	}
	message.Set(field, protoreflect.ValueOfBytes(append([]byte(nil), value[:]...)))
	return nil
}

// readTimestamp 解析 RFC3339 时间，并校验 Proto Timestamp 范围和纳秒精度
func (d *protoDecoder) readTimestamp(node *jsonNode, message protoreflect.Message) error {
	if node.kind != nodeString {
		return errors.New("Timestamp must be a JSON string")
	}
	value, err := time.Parse(time.RFC3339Nano, node.text)
	if err != nil {
		return err
	}
	seconds := value.Unix()
	if seconds < -62135596800 || seconds > 253402300799 {
		return errors.New("Timestamp out of range")
	}
	if point := strings.LastIndexByte(node.text, '.'); point >= 0 {
		zone := strings.LastIndexAny(node.text, "Z-+")
		if zone >= point && zone-point-1 > 9 {
			return errors.New("Timestamp has more than 9 fractional digits")
		}
	}
	message.Set(message.Descriptor().Fields().ByNumber(1), protoreflect.ValueOfInt64(seconds))
	message.Set(message.Descriptor().Fields().ByNumber(2), protoreflect.ValueOfInt32(int32(value.Nanosecond())))
	return nil
}

// readDuration 解析 ProtoJSON Duration 字符串并写入秒和纳秒字段
func (d *protoDecoder) readDuration(node *jsonNode, message protoreflect.Message) error {
	if node.kind != nodeString {
		return errors.New("Duration must be a JSON string ending in s")
	}
	seconds, nanos, ok := parseDuration(node.text)
	if !ok || seconds < -315576000000 || seconds > 315576000000 {
		return errors.New("invalid Duration")
	}
	message.Set(message.Descriptor().Fields().ByNumber(1), protoreflect.ValueOfInt64(seconds))
	message.Set(message.Descriptor().Fields().ByNumber(2), protoreflect.ValueOfInt32(nanos))
	return nil
}

// parseDuration 精确解析最长 9 位小数的 Duration，避免 float64 精度损失
func parseDuration(value string) (int64, int32, bool) {
	if len(value) < 2 || value[len(value)-1] != 's' {
		return 0, 0, false
	}
	value = value[:len(value)-1]
	negative := false
	if value[0] == '-' || value[0] == '+' {
		negative = value[0] == '-'
		value = value[1:]
	}
	if value == "" {
		return 0, 0, false
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || len(parts) == 2 && len(parts[1]) > 9 || len(parts) == 2 && parts[0] == "" && parts[1] == "" {
		return 0, 0, false
	}
	secondsText := parts[0]
	if secondsText == "" {
		secondsText = "0"
	}
	if len(secondsText) > 1 && secondsText[0] == '0' {
		return 0, 0, false
	}
	seconds, err := strconv.ParseInt(secondsText, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	var nanos int64
	if len(parts) == 2 {
		fraction := parts[1]
		if fraction != "" {
			for _, character := range fraction {
				if character < '0' || character > '9' {
					return 0, 0, false
				}
			}
			fraction += strings.Repeat("0", 9-len(fraction))
			nanos, err = strconv.ParseInt(fraction, 10, 32)
			if err != nil {
				return 0, 0, false
			}
		}
	}
	if negative {
		seconds = -seconds
		nanos = -nanos
	}
	return seconds, int32(nanos), true
}

// readStruct 将 JSON 对象写入 Struct.fields Map
func (d *protoDecoder) readStruct(node *jsonNode, message protoreflect.Message) error {
	field := message.Descriptor().Fields().ByNumber(1)
	return d.readMap(node, message.Mutable(field).Map(), field)
}

// readListValue 将 JSON 数组写入 ListValue.values
func (d *protoDecoder) readListValue(node *jsonNode, message protoreflect.Message) error {
	field := message.Descriptor().Fields().ByNumber(1)
	return d.readList(node, message.Mutable(field).List(), field)
}

// readKnownValue 根据 JSON 节点类型选择并设置 Value.kind oneof
func (d *protoDecoder) readKnownValue(node *jsonNode, message protoreflect.Message) error {
	fields := message.Descriptor().Fields()
	var field protoreflect.FieldDescriptor
	var value protoreflect.Value
	switch node.kind {
	case nodeNull:
		field = fields.ByNumber(1)
		value = protoreflect.ValueOfEnum(0)
	case nodeNumber:
		field = fields.ByNumber(2)
		parsed, err := strconv.ParseFloat(node.text, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return errors.New("invalid google.protobuf.Value number")
		}
		value = protoreflect.ValueOfFloat64(parsed)
	case nodeString:
		field = fields.ByNumber(3)
		value = protoreflect.ValueOfString(node.text)
	case nodeBool:
		field = fields.ByNumber(4)
		value = protoreflect.ValueOfBool(node.boolean)
	case nodeObject:
		field = fields.ByNumber(5)
		value = message.NewField(field)
		if err := d.readStruct(node, value.Message()); err != nil {
			return err
		}
	case nodeArray:
		field = fields.ByNumber(6)
		value = message.NewField(field)
		if err := d.readListValue(node, value.Message()); err != nil {
			return err
		}
	default:
		return errors.New("invalid google.protobuf.Value")
	}
	message.Set(field, value)
	return nil
}

// readFieldMask 将逗号分隔的 camelCase 路径转换为 Proto snake_case 路径列表
func (d *protoDecoder) readFieldMask(node *jsonNode, message protoreflect.Message) error {
	if node.kind != nodeString {
		return errors.New("FieldMask must be a JSON string")
	}
	if strings.TrimSpace(node.text) == "" {
		return nil
	}
	list := message.Mutable(message.Descriptor().Fields().ByNumber(1)).List()
	for _, path := range strings.Split(node.text, ",") {
		if strings.Contains(path, "_") {
			return fmt.Errorf("invalid FieldMask path %q", path)
		}
		converted := protoJSONSnakeCase(path)
		if !protoreflect.FullName(converted).IsValid() {
			return fmt.Errorf("invalid FieldMask path %q", path)
		}
		list.Append(protoreflect.ValueOfString(converted))
	}
	return nil
}

// readAny 解析 type_url，解码内嵌消息，并将确定性 Protobuf 二进制写回 Any.value
func (d *protoDecoder) readAny(node *jsonNode, message protoreflect.Message) error {
	if node.kind != nodeObject {
		return errors.New("Any must be a JSON object")
	}
	var typeURL string
	var valueNode *jsonNode
	for _, member := range node.object {
		switch member.name {
		case "@type":
			if typeURL != "" {
				return errors.New("duplicate Any @type field")
			}
			if member.value.kind != nodeString || member.value.text == "" {
				return errors.New("Any @type must be a non-empty string")
			}
			typeURL = member.value.text
		case "value":
			if valueNode != nil {
				return errors.New("duplicate Any value field")
			}
			valueNode = member.value
		}
	}
	if typeURL == "" {
		if len(node.object) == 0 || d.options.DiscardUnknown {
			return nil
		}
		return errors.New("missing Any @type field")
	}
	messageType, err := d.options.Resolver.FindMessageByURL(typeURL)
	if err != nil {
		return fmt.Errorf("resolve Any type %q: %w", typeURL, err)
	}
	embedded := messageType.New()
	if isSpecialMessage(embedded.Descriptor().FullName()) {
		for _, member := range node.object {
			if member.name != "@type" && member.name != "value" && !d.options.DiscardUnknown {
				return fmt.Errorf("unknown Any field %q", member.name)
			}
		}
		if valueNode == nil && embedded.Descriptor().FullName() != "google.protobuf.Empty" {
			return errors.New("missing Any value field")
		}
		if valueNode != nil {
			if err := d.readMessage(valueNode, embedded); err != nil {
				return err
			}
		}
	} else {
		members := make([]objectMember, 0, len(node.object)-1)
		for _, member := range node.object {
			if member.name != "@type" {
				members = append(members, member)
			}
		}
		if err := d.readMessage(&jsonNode{kind: nodeObject, object: members}, embedded); err != nil {
			return err
		}
	}
	raw, err := proto.MarshalOptions{AllowPartial: true, Deterministic: true}.Marshal(embedded.Interface())
	if err != nil {
		return err
	}
	fields := message.Descriptor().Fields()
	message.Set(fields.ByNumber(1), protoreflect.ValueOfString(typeURL))
	message.Set(fields.ByNumber(2), protoreflect.ValueOfBytes(raw))
	return nil
}

// parseSigned 按目标位宽解析有符号整数，并接受值为整数的指数或零小数形式
func parseSigned(node *jsonNode, bitSize int) (int64, error) {
	text, ok := numericText(node)
	if !ok {
		return 0, errors.New("expected number or numeric string")
	}
	if !strings.ContainsAny(text, ".eE") {
		return strconv.ParseInt(text, 10, bitSize)
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value {
		return 0, errors.New("expected integral number")
	}
	if bitSize == 32 && (value < math.MinInt32 || value > math.MaxInt32) || bitSize == 64 && (value < math.MinInt64 || value >= -math.MinInt64) {
		return 0, errors.New("signed integer out of range")
	}
	return int64(value), nil
}

// parseUnsigned 按目标位宽解析无符号整数，并拒绝负数、非整数和越界值
func parseUnsigned(node *jsonNode, bitSize int) (uint64, error) {
	text, ok := numericText(node)
	if !ok {
		return 0, errors.New("expected number or numeric string")
	}
	if !strings.ContainsAny(text, ".eE") {
		return strconv.ParseUint(text, 10, bitSize)
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < 0 {
		return 0, errors.New("expected non-negative integral number")
	}
	if bitSize == 32 && value > math.MaxUint32 || bitSize == 64 && value >= math.Exp2(64) {
		return 0, errors.New("unsigned integer out of range")
	}
	return uint64(value), nil
}

// parseFloat 解析普通浮点数和 ProtoJSON 允许的 NaN、正负 Infinity
func parseFloat(node *jsonNode, bitSize int) (float64, error) {
	text, ok := numericText(node)
	if !ok {
		return 0, errors.New("expected number or numeric string")
	}
	switch text {
	case "NaN":
		return math.NaN(), nil
	case "Infinity":
		return math.Inf(1), nil
	case "-Infinity":
		return math.Inf(-1), nil
	default:
		return strconv.ParseFloat(text, bitSize)
	}
}

// numericText 提取数字节点或合法数字字符串的原文
func numericText(node *jsonNode) (string, bool) {
	if node.kind != nodeNumber && node.kind != nodeString {
		return "", false
	}
	if node.text == "" || strings.TrimSpace(node.text) != node.text {
		return "", false
	}
	if node.kind == nodeString && !isJSONNumber(node.text) && node.text != "NaN" && node.text != "Infinity" && node.text != "-Infinity" {
		return "", false
	}
	return node.text, true
}

// isJSONNumber 按 JSON number 语法校验完整字符串，不接受前导加号和非法前导零
func isJSONNumber(value string) bool {
	if value == "" {
		return false
	}
	index := 0
	if value[index] == '-' {
		index++
		if index == len(value) {
			return false
		}
	}
	if value[index] == '0' {
		index++
	} else if value[index] >= '1' && value[index] <= '9' {
		for index < len(value) && value[index] >= '0' && value[index] <= '9' {
			index++
		}
	} else {
		return false
	}
	if index < len(value) && value[index] == '.' {
		index++
		start := index
		for index < len(value) && value[index] >= '0' && value[index] <= '9' {
			index++
		}
		if start == index {
			return false
		}
	}
	if index < len(value) && (value[index] == 'e' || value[index] == 'E') {
		index++
		if index < len(value) && (value[index] == '+' || value[index] == '-') {
			index++
		}
		start := index
		for index < len(value) && value[index] >= '0' && value[index] <= '9' {
			index++
		}
		if start == index {
			return false
		}
	}
	return index == len(value)
}

// decodeBase64 解码 ProtoJSON 允许的标准或 URL-safe Base64，并兼容无填充形式
func decodeBase64(value string) ([]byte, error) {
	encoding := base64.StdEncoding
	if strings.ContainsAny(value, "-_") {
		encoding = base64.URLEncoding
	}
	if len(value)%4 != 0 {
		encoding = encoding.WithPadding(base64.NoPadding)
	}
	return encoding.DecodeString(value)
}

// parseMapKey 按 Map key 的 Proto kind 解析 JSON 对象成员名称
func parseMapKey(value string, field protoreflect.FieldDescriptor) (protoreflect.MapKey, error) {
	switch field.Kind() {
	case protoreflect.StringKind:
		return protoreflect.ValueOfString(value).MapKey(), nil
	case protoreflect.BoolKind:
		parsed, err := strconv.ParseBool(value)
		return protoreflect.ValueOfBool(parsed).MapKey(), err
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		parsed, err := strconv.ParseInt(value, 10, 32)
		return protoreflect.ValueOfInt32(int32(parsed)).MapKey(), err
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		parsed, err := strconv.ParseInt(value, 10, 64)
		return protoreflect.ValueOfInt64(parsed).MapKey(), err
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		parsed, err := strconv.ParseUint(value, 10, 32)
		return protoreflect.ValueOfUint32(uint32(parsed)).MapKey(), err
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		parsed, err := strconv.ParseUint(value, 10, 64)
		return protoreflect.ValueOfUint64(parsed).MapKey(), err
	default:
		return protoreflect.MapKey{}, fmt.Errorf("invalid map key kind %s", field.Kind())
	}
}

// isKnownValueField 判断字段消息类型是否为 google.protobuf.Value
func isKnownValueField(field protoreflect.FieldDescriptor) bool {
	return field.Message() != nil && field.Message().FullName() == "google.protobuf.Value"
}

// isNullValueField 判断字段枚举类型是否为 google.protobuf.NullValue
func isNullValueField(field protoreflect.FieldDescriptor) bool {
	return field.Enum() != nil && field.Enum().FullName() == "google.protobuf.NullValue"
}

// enter 增加消息递归深度，并在超过配置上限时返回错误
func (d *protoDecoder) enter() error {
	d.depth++
	if d.depth > d.options.RecursionLimit {
		return errors.New("sonic: exceeded max recursion depth")
	}
	return nil
}

// leave 在完成当前消息解码后恢复递归深度
func (d *protoDecoder) leave() {
	d.depth--
}
