//go:build go1.27

package sonic

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	jsonsonic "github.com/bytedance/sonic"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const uuidMessageName protoreflect.FullName = "uuid.UUID"

// protoEncoder 按 Proto3 Descriptor 递归写入 JSON，并复用 Sonic 处理字符串转义
type protoEncoder struct {
	// options 控制字段名称、默认值、枚举和 Any 解析行为
	options Options
	// json 提供经过安全配置的字符串 JSON 编码能力
	json jsonsonic.API
	// metadata 缓存 Descriptor 派生的字段名和枚举名
	metadata *descriptorMetadataCache
	// buffer 保存最终 JSON 字节，编码过程中不构造 map[string]any
	buffer bytes.Buffer
	// depth 记录当前 Proto 消息递归深度
	depth int
}

// marshalProto3 使用 Proto3 反射语义编码完整消息
func marshalProto3(message protoreflect.Message, options Options, jsonAPI jsonsonic.API, metadata *descriptorMetadataCache) ([]byte, error) {
	encoder := protoEncoder{options: options, json: jsonAPI, metadata: metadata}
	if err := encoder.writeMessage(message); err != nil {
		return nil, err
	}
	return encoder.buffer.Bytes(), nil
}

// writeMessage 根据消息完整名称分派普通消息、UUID 和 Well-Known Types
func (e *protoEncoder) writeMessage(message protoreflect.Message) error {
	if err := e.enter(); err != nil {
		return err
	}
	defer e.leave()

	name := message.Descriptor().FullName()
	switch name {
	case uuidMessageName:
		return e.writeUUID(message)
	case "google.protobuf.Any":
		return e.writeAny(message)
	case "google.protobuf.Timestamp":
		return e.writeTimestamp(message)
	case "google.protobuf.Duration":
		return e.writeDuration(message)
	case "google.protobuf.BoolValue", "google.protobuf.Int32Value", "google.protobuf.Int64Value",
		"google.protobuf.UInt32Value", "google.protobuf.UInt64Value", "google.protobuf.FloatValue",
		"google.protobuf.DoubleValue", "google.protobuf.StringValue", "google.protobuf.BytesValue":
		field := message.Descriptor().Fields().ByNumber(1)
		return e.writeSingular(message.Get(field), field)
	case "google.protobuf.Empty":
		e.buffer.WriteString("{}")
		return nil
	case "google.protobuf.Struct":
		return e.writeStruct(message)
	case "google.protobuf.ListValue":
		return e.writeListValue(message)
	case "google.protobuf.Value":
		return e.writeKnownValue(message)
	case "google.protobuf.FieldMask":
		return e.writeFieldMask(message)
	}
	return e.writeOrdinaryMessage(message)
}

// writeOrdinaryMessage 按 Descriptor 字段顺序编码普通 Proto3 消息对象
func (e *protoEncoder) writeOrdinaryMessage(message protoreflect.Message) error {
	e.buffer.WriteByte('{')
	if err := e.writeMessageFields(message, true); err != nil {
		return err
	}
	e.buffer.WriteByte('}')
	return nil
}

// writeMessageFields 按 Descriptor 顺序选择并写出普通消息字段
// first 表示当前 JSON 对象尚未写入任何成员，Any 会传入 false 以衔接 @type
func (e *protoEncoder) writeMessageFields(message protoreflect.Message, first bool) error {
	metadata := e.metadata.message(message.Descriptor())
	if err := metadata.encodeNames(e.json); err != nil {
		return err
	}
	for index := range metadata.fields {
		entry := &metadata.fields[index]
		field := entry.field
		value, emit := e.messageFieldValue(message, entry)
		if !emit {
			continue
		}
		if !first {
			e.buffer.WriteByte(',')
		}
		first = false
		name := entry.jsonName
		if e.options.UseProtoNames {
			name = entry.protoName
		}
		e.buffer.Write(name)
		e.buffer.WriteByte(':')
		if !value.IsValid() {
			e.buffer.WriteString("null")
			continue
		}
		if err := e.writeValue(value, entry); err != nil {
			return fmt.Errorf("sonic: field %s: %w", field.FullName(), err)
		}
	}
	return nil
}

// messageFieldValue 判断字段是否输出，并返回 presence 和默认值规则对应的值
func (e *protoEncoder) messageFieldValue(message protoreflect.Message, entry *fieldMetadata) (protoreflect.Value, bool) {
	field := entry.field
	if message.Has(field) {
		return message.Get(field), true
	}
	if entry.hasOneof || !e.options.EmitUnpopulated && !e.options.EmitDefaultValues {
		return protoreflect.Value{}, false
	}
	if entry.hasPresence {
		return protoreflect.Value{}, e.options.EmitUnpopulated
	}
	return message.Get(field), true
}

// writeValue 根据字段基数分派单值、列表或 Map 编码
func (e *protoEncoder) writeValue(value protoreflect.Value, entry *fieldMetadata) error {
	if entry.isList {
		return e.writeList(value.List(), entry.field)
	}
	if entry.isMap {
		return e.writeMap(value.Map(), entry.mapKey, entry.mapValue)
	}
	return e.writeSingularKind(value, entry.field, entry.kind)
}

// writeSingular 按 Proto3 kind 编码标量、枚举或嵌套消息
func (e *protoEncoder) writeSingular(value protoreflect.Value, field protoreflect.FieldDescriptor) error {
	return e.writeSingularKind(value, field, field.Kind())
}

// writeSingularKind 使用已知 kind 编码标量、枚举或嵌套消息
func (e *protoEncoder) writeSingularKind(value protoreflect.Value, field protoreflect.FieldDescriptor, kind protoreflect.Kind) error {
	switch kind {
	case protoreflect.BoolKind:
		e.writeBool(value.Bool())
	case protoreflect.StringKind:
		return e.writeString(value.String())
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		e.writeInt(value.Int())
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		e.writeUint(value.Uint())
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		e.writeQuotedInt(value.Int())
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		e.writeQuotedUint(value.Uint())
	case protoreflect.FloatKind:
		return e.writeFloat(value.Float(), 32)
	case protoreflect.DoubleKind:
		return e.writeFloat(value.Float(), 64)
	case protoreflect.BytesKind:
		e.writeBytes(value.Bytes())
	case protoreflect.EnumKind:
		if field.Enum().FullName() == "google.protobuf.NullValue" {
			e.buffer.WriteString("null")
			return nil
		}
		if e.options.UseEnumNumbers {
			e.writeInt(int64(value.Enum()))
			return nil
		}
		metadata, err := e.metadata.enum(field.Enum(), e.json)
		if err != nil {
			return err
		}
		encoded, ok := metadata.values[value.Enum()]
		if !ok {
			e.writeInt(int64(value.Enum()))
			return nil
		}
		e.buffer.Write(encoded)
		return nil
	case protoreflect.MessageKind:
		return e.writeMessage(value.Message())
	default:
		return fmt.Errorf("unsupported Proto3 kind %s", kind)
	}
	return nil
}

// writeList 按元素顺序编码 repeated 字段
func (e *protoEncoder) writeList(list protoreflect.List, field protoreflect.FieldDescriptor) error {
	e.buffer.WriteByte('[')
	for index := 0; index < list.Len(); index++ {
		if index > 0 {
			e.buffer.WriteByte(',')
		}
		if err := e.writeSingular(list.Get(index), field); err != nil {
			return err
		}
	}
	e.buffer.WriteByte(']')
	return nil
}

// writeMap 按反射遍历顺序编码 Map；JSON 对象成员顺序不构成语义
func (e *protoEncoder) writeMap(mapping protoreflect.Map, keyField, valueField protoreflect.FieldDescriptor) error {
	e.buffer.WriteByte('{')
	first := true
	var encodeErr error
	mapping.Range(func(key protoreflect.MapKey, value protoreflect.Value) bool {
		if !first {
			e.buffer.WriteByte(',')
		}
		first = false
		if err := e.writeMapKey(key, keyField); err != nil {
			encodeErr = err
			return false
		}
		e.buffer.WriteByte(':')
		if err := e.writeSingular(value, valueField); err != nil {
			encodeErr = err
			return false
		}
		return true
	})
	if encodeErr != nil {
		return encodeErr
	}
	e.buffer.WriteByte('}')
	return nil
}

// writeMapKey 将 Proto Map 键直接写为 JSON 对象成员名称
func (e *protoEncoder) writeMapKey(key protoreflect.MapKey, field protoreflect.FieldDescriptor) error {
	switch field.Kind() {
	case protoreflect.BoolKind:
		e.buffer.WriteByte('"')
		e.writeBool(key.Bool())
		e.buffer.WriteByte('"')
		return nil
	case protoreflect.StringKind:
		return e.writeString(key.String())
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind, protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		e.writeQuotedInt(key.Int())
		return nil
	default:
		e.writeQuotedUint(key.Uint())
		return nil
	}
}

// writeUUID 将底层 16 字节 UUID 直接编码为规范 UUID 字符串
func (e *protoEncoder) writeUUID(message protoreflect.Message) error {
	field := message.Descriptor().Fields().ByNumber(1)
	raw := message.Get(field).Bytes()
	if len(raw) == 0 {
		e.buffer.WriteString(`""`)
		return nil
	}
	if len(raw) != 16 {
		return errors.New("uuid value must be exactly 16 bytes")
	}
	const hexadecimal = "0123456789abcdef"
	var encoded [38]byte
	encoded[0] = '"'
	encoded[len(encoded)-1] = '"'
	output := 1
	for index, value := range raw {
		if index == 4 || index == 6 || index == 8 || index == 10 {
			encoded[output] = '-'
			output++
		}
		encoded[output] = hexadecimal[value>>4]
		encoded[output+1] = hexadecimal[value&0x0f]
		output += 2
	}
	e.buffer.Write(encoded[:])
	return nil
}

// writeTimestamp 校验范围并按 ProtoJSON 的 UTC RFC3339 形式编码 Timestamp
func (e *protoEncoder) writeTimestamp(message protoreflect.Message) error {
	seconds := message.Get(message.Descriptor().Fields().ByNumber(1)).Int()
	nanos := message.Get(message.Descriptor().Fields().ByNumber(2)).Int()
	if seconds < -62135596800 || seconds > 253402300799 || nanos < 0 || nanos > 999999999 {
		return errors.New("invalid google.protobuf.Timestamp")
	}
	var scratch [32]byte
	encoded := time.Unix(seconds, nanos).UTC().AppendFormat(scratch[:0], "2006-01-02T15:04:05.000000000")
	switch {
	case nanos == 0:
		encoded = encoded[:len(encoded)-10]
	case nanos%1000000 == 0:
		encoded = encoded[:len(encoded)-6]
	case nanos%1000 == 0:
		encoded = encoded[:len(encoded)-3]
	}
	e.buffer.WriteByte('"')
	e.buffer.Write(encoded)
	e.buffer.WriteString(`Z"`)
	return nil
}

// writeDuration 校验秒和纳秒符号、范围，并输出 ProtoJSON Duration 字符串
func (e *protoEncoder) writeDuration(message protoreflect.Message) error {
	seconds := message.Get(message.Descriptor().Fields().ByNumber(1)).Int()
	nanos := message.Get(message.Descriptor().Fields().ByNumber(2)).Int()
	if seconds < -315576000000 || seconds > 315576000000 || nanos < -999999999 || nanos > 999999999 || seconds > 0 && nanos < 0 || seconds < 0 && nanos > 0 {
		return errors.New("invalid google.protobuf.Duration")
	}
	negative := false
	if seconds < 0 || nanos < 0 {
		negative = true
		seconds = -seconds
		nanos = -nanos
	}
	e.buffer.WriteByte('"')
	if negative {
		e.buffer.WriteByte('-')
	}
	e.writeInt(seconds)
	if nanos != 0 {
		e.buffer.WriteByte('.')
		e.writeFraction(uint32(nanos))
	}
	e.buffer.WriteString(`s"`)
	return nil
}

// writeStruct 将 Struct.fields 直接编码为 JSON 对象
func (e *protoEncoder) writeStruct(message protoreflect.Message) error {
	field := message.Descriptor().Fields().ByNumber(1)
	return e.writeMap(message.Get(field).Map(), field.MapKey(), field.MapValue())
}

// writeListValue 将 ListValue.values 直接编码为 JSON 数组
func (e *protoEncoder) writeListValue(message protoreflect.Message) error {
	field := message.Descriptor().Fields().ByNumber(1)
	return e.writeList(message.Get(field).List(), field)
}

// writeKnownValue 根据 Value.kind oneof 输出对应的原生 JSON 值
func (e *protoEncoder) writeKnownValue(message protoreflect.Message) error {
	oneof := message.Descriptor().Oneofs().ByName("kind")
	field := message.WhichOneof(oneof)
	if field == nil {
		return errors.New("google.protobuf.Value has no kind set")
	}
	return e.writeSingular(message.Get(field), field)
}

// writeFieldMask 校验路径可逆性，并将 snake_case 路径编码为逗号分隔的 camelCase 字符串
func (e *protoEncoder) writeFieldMask(message protoreflect.Message) error {
	paths := message.Get(message.Descriptor().Fields().ByNumber(1)).List()
	e.buffer.WriteByte('"')
	for index := 0; index < paths.Len(); index++ {
		path := paths.Get(index).String()
		if !protoreflect.FullName(path).IsValid() {
			return fmt.Errorf("invalid FieldMask path %q", path)
		}
		if !isReversibleFieldMaskPath(path) {
			return fmt.Errorf("irreversible FieldMask path %q", path)
		}
		if index > 0 {
			e.buffer.WriteByte(',')
		}
		e.writeFieldMaskPath(path)
	}
	e.buffer.WriteByte('"')
	return nil
}

// isReversibleFieldMaskPath 判断 snake_case 路径能否无损往返 ProtoJSON camelCase
func isReversibleFieldMaskPath(path string) bool {
	for index := 0; index < len(path); index++ {
		character := path[index]
		if character >= 'A' && character <= 'Z' {
			return false
		}
		if character == '_' {
			index++
			if index == len(path) || path[index] < 'a' || path[index] > 'z' {
				return false
			}
		}
	}
	return true
}

// writeFieldMaskPath 将已验证的 snake_case 路径直接写为 camelCase
func (e *protoEncoder) writeFieldMaskPath(path string) {
	upper := false
	for index := 0; index < len(path); index++ {
		character := path[index]
		if character == '_' {
			upper = true
			continue
		}
		if upper {
			character -= 'a' - 'A'
			upper = false
		}
		e.buffer.WriteByte(character)
	}
}

// writeAny 解析 Any.value 的二进制消息，并按内嵌消息类型输出展开或 value 包装形式
func (e *protoEncoder) writeAny(message protoreflect.Message) error {
	fields := message.Descriptor().Fields()
	typeURL := message.Get(fields.ByNumber(1)).String()
	raw := message.Get(fields.ByNumber(2)).Bytes()
	if typeURL == "" {
		if len(raw) != 0 {
			return errors.New("google.protobuf.Any type_url is not set")
		}
		e.buffer.WriteString("{}")
		return nil
	}
	messageType, err := e.options.Resolver.FindMessageByURL(typeURL)
	if err != nil {
		return fmt.Errorf("resolve Any type %q: %w", typeURL, err)
	}
	embedded := messageType.New()
	if err := (proto.UnmarshalOptions{AllowPartial: true}).Unmarshal(raw, embedded.Interface()); err != nil {
		return fmt.Errorf("unmarshal Any value: %w", err)
	}

	e.buffer.WriteByte('{')
	if err := e.writeString("@type"); err != nil {
		return err
	}
	e.buffer.WriteByte(':')
	if err := e.writeString(typeURL); err != nil {
		return err
	}
	if isSpecialMessage(embedded.Descriptor().FullName()) {
		e.buffer.WriteByte(',')
		if err := e.writeString("value"); err != nil {
			return err
		}
		e.buffer.WriteByte(':')
		if err := e.writeMessage(embedded); err != nil {
			return err
		}
		e.buffer.WriteByte('}')
		return nil
	}

	if err := e.writeMessageFields(embedded, false); err != nil {
		return err
	}
	e.buffer.WriteByte('}')
	return nil
}

// isSpecialMessage 判断消息是否使用非普通对象形式的 JSON 表示
func isSpecialMessage(name protoreflect.FullName) bool {
	switch name {
	case uuidMessageName, "google.protobuf.Any", "google.protobuf.Timestamp", "google.protobuf.Duration",
		"google.protobuf.BoolValue", "google.protobuf.Int32Value", "google.protobuf.Int64Value",
		"google.protobuf.UInt32Value", "google.protobuf.UInt64Value", "google.protobuf.FloatValue",
		"google.protobuf.DoubleValue", "google.protobuf.StringValue", "google.protobuf.BytesValue",
		"google.protobuf.Struct", "google.protobuf.ListValue", "google.protobuf.Value", "google.protobuf.FieldMask", "google.protobuf.Empty":
		return true
	default:
		return false
	}
}

// protoJSONSnakeCase 将 JSON camelCase 路径转换回 Proto snake_case
func protoJSONSnakeCase(value string) string {
	var output strings.Builder
	for _, character := range value {
		if character >= 'A' && character <= 'Z' {
			output.WriteByte('_')
			character += 'a' - 'A'
		}
		output.WriteRune(character)
	}
	return output.String()
}

// writeFloat 编码普通浮点数和 ProtoJSON 规定的非有限特殊值
func (e *protoEncoder) writeFloat(value float64, bitSize int) error {
	switch {
	case math.IsNaN(value):
		return e.writeString("NaN")
	case math.IsInf(value, 1):
		return e.writeString("Infinity")
	case math.IsInf(value, -1):
		return e.writeString("-Infinity")
	default:
		var scratch [32]byte
		format := byte('f')
		absolute := math.Abs(value)
		if absolute != 0 && (bitSize == 64 && (absolute < 1e-6 || absolute >= 1e21) ||
			bitSize == 32 && (float32(absolute) < 1e-6 || float32(absolute) >= 1e21)) {
			format = 'e'
		}
		encoded := strconv.AppendFloat(scratch[:0], value, format, -1, bitSize)
		if length := len(encoded); format == 'e' && length >= 4 && encoded[length-4] == 'e' && encoded[length-3] == '-' && encoded[length-2] == '0' {
			encoded[length-2] = encoded[length-1]
			encoded = encoded[:length-1]
		}
		e.buffer.Write(encoded)
		return nil
	}
}

// writeBool 直接写入 JSON 布尔值固定 token
func (e *protoEncoder) writeBool(value bool) {
	if value {
		e.buffer.WriteString("true")
		return
	}
	e.buffer.WriteString("false")
}

// writeInt 使用栈内空间写入十进制有符号整数
func (e *protoEncoder) writeInt(value int64) {
	var scratch [20]byte
	e.buffer.Write(strconv.AppendInt(scratch[:0], value, 10))
}

// writeUint 使用栈内空间写入十进制无符号整数
func (e *protoEncoder) writeUint(value uint64) {
	var scratch [20]byte
	e.buffer.Write(strconv.AppendUint(scratch[:0], value, 10))
}

// writeFraction 按 ProtoJSON 规则以 3、6 或 9 位精度写入纳秒小数部分
func (e *protoEncoder) writeFraction(nanos uint32) {
	digits := 9
	if nanos%1000000 == 0 {
		digits = 3
	} else if nanos%1000 == 0 {
		digits = 6
	}
	var encoded [9]byte
	for index := len(encoded) - 1; index >= 0; index-- {
		encoded[index] = byte(nanos%10) + '0'
		nanos /= 10
	}
	e.buffer.Write(encoded[:digits])
}

// writeQuotedInt 按 ProtoJSON 规则将 64 位有符号整数写为字符串
func (e *protoEncoder) writeQuotedInt(value int64) {
	e.buffer.WriteByte('"')
	e.writeInt(value)
	e.buffer.WriteByte('"')
}

// writeQuotedUint 按 ProtoJSON 规则将 64 位无符号整数写为字符串
func (e *protoEncoder) writeQuotedUint(value uint64) {
	e.buffer.WriteByte('"')
	e.writeUint(value)
	e.buffer.WriteByte('"')
}

// writeBytes 将普通 Proto bytes 直接编码为带引号的 Base64 字符串
func (e *protoEncoder) writeBytes(value []byte) {
	encodedLength := base64.StdEncoding.EncodedLen(len(value))
	e.buffer.Grow(encodedLength + 2)
	encoded := e.buffer.AvailableBuffer()
	encoded = append(encoded, '"')
	payloadStart := len(encoded)
	encoded = encoded[:payloadStart+encodedLength]
	base64.StdEncoding.Encode(encoded[payloadStart:], value)
	encoded = append(encoded, '"')
	e.buffer.Write(encoded)
}

// writeString 校验 UTF-8 后使用 Sonic 完成 JSON 字符串转义
func (e *protoEncoder) writeString(value string) error {
	if !utf8.ValidString(value) {
		return errors.New("sonic: invalid UTF-8 string")
	}
	encoded, err := e.json.Marshal(value)
	if err != nil {
		return err
	}
	e.buffer.Write(encoded)
	return nil
}

// enter 增加消息递归深度，并在超过配置上限时返回错误
func (e *protoEncoder) enter() error {
	e.depth++
	if e.depth > e.options.RecursionLimit {
		return errors.New("sonic: exceeded max recursion depth")
	}
	return nil
}

// leave 在完成当前消息编码后恢复递归深度
func (e *protoEncoder) leave() {
	e.depth--
}
