//go:build go1.27

package sonic

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	uuidpb "github.com/azicen/kratos-extension/types/uuid"
	jsonsonic "github.com/bytedance/sonic"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// benchmarkMessage 是与 Proto3 基准消息字段语义一致的普通 Go 结构体
type benchmarkMessage struct {
	// PageSize 表示分页大小
	PageSize int32 `json:"pageSize"`
	// TotalCount 表示记录总数；string 标签使 JSON 形式与 ProtoJSON 的 int64 保持一致
	TotalCount int64 `json:"totalCount,string"`
	// Status 表示状态名称
	Status string `json:"status"`
}

var benchmarkJSON = []byte(`{"pageSize":20,"totalCount":"9223372036854775807","status":"STATUS_ACTIVE"}`)

const (
	bytesBenchmarkPayloadSize = 16 * 1024
	largeBenchmarkChildCount  = 100
	mapBenchmarkEntryCount    = 256
	numericBenchmarkItemCount = 1024
	wideBenchmarkFieldCount   = 64
)

// largeBenchmarkMessage 表示包含集合、Map 和嵌套实体的大型普通 Go 消息
type largeBenchmarkMessage struct {
	ID         string                         `json:"id"`
	Title      string                         `json:"title"`
	Enabled    bool                           `json:"enabled"`
	TotalCount int64                          `json:"totalCount,string"`
	Children   []largeBenchmarkChild          `json:"children"`
	ChildMap   map[string]largeBenchmarkChild `json:"childMap"`
	Labels     map[string]string              `json:"labels"`
	Scores     map[string]int32               `json:"scores"`
}

// largeBenchmarkChild 表示大型基准中的嵌套子实体
type largeBenchmarkChild struct {
	Sequence    int32    `json:"sequence"`
	Total       int64    `json:"total,string"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Active      bool     `json:"active,omitempty"`
	Status      string   `json:"status"`
	Numbers     []int32  `json:"numbers"`
	Tags        []string `json:"tags"`
}

// BenchmarkCodecMarshal 对比普通 Go struct 与动态 Proto3 消息的编码性能
func BenchmarkCodecMarshal(b *testing.B) {
	codec := NewCodec()
	goValue := benchmarkMessage{
		PageSize:   20,
		TotalCount: 9223372036854775807,
		Status:     "STATUS_ACTIVE",
	}
	protoValue := newBenchmarkProtoMessage(b)

	b.Run("NativeSonic", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := jsonsonic.ConfigStd.Marshal(goValue); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("NativeJSONV2", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := jsonv2.Marshal(goValue); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("GoStruct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := codec.Marshal(goValue); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("NativeProtoJSON", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := protojson.Marshal(protoValue); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Proto3", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := codec.Marshal(protoValue); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkCodecMarshalNestedUUID 测量嵌套 UUID 消息的编码性能
func BenchmarkCodecMarshalNestedUUID(b *testing.B) {
	message := dynamicpb.NewMessage(uuidContainerDescriptor(b))
	field := message.Descriptor().Fields().ByName("id")
	message.Set(field, protoreflect.ValueOfMessage(uuidpb.Wrap(uuid.MustParse("01993b93-789a-7def-8123-456789abcdef")).ProtoReflect()))
	codec := NewCodec()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := codec.Marshal(message); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCodecUnmarshal 对比普通 Go struct 与动态 Proto3 消息的解码性能
func BenchmarkCodecUnmarshal(b *testing.B) {
	codec := NewCodec()

	b.Run("NativeSonic", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var target benchmarkMessage
			if err := jsonsonic.ConfigStd.Unmarshal(benchmarkJSON, &target); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("NativeJSONV2", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var target benchmarkMessage
			if err := jsonv2.Unmarshal(benchmarkJSON, &target); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("GoStruct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var target benchmarkMessage
			if err := codec.Unmarshal(benchmarkJSON, &target); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("NativeProtoJSON", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			message := newRootMessage(b)
			if err := protojson.Unmarshal(benchmarkJSON, message); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Proto3", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			message := newRootMessage(b)
			if err := codec.Unmarshal(benchmarkJSON, message); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// newBenchmarkProtoMessage 创建与 benchmarkMessage 数据语义一致的动态 Proto3 消息
func newBenchmarkProtoMessage(b testing.TB) *dynamicpb.Message {
	b.Helper()
	message := newRootMessage(b)
	setField(message, "page_size", protoreflect.ValueOfInt32(20))
	setField(message, "total_count", protoreflect.ValueOfInt64(9223372036854775807))
	setField(message, "status", protoreflect.ValueOfEnum(1))
	return message
}

// BenchmarkCodecMarshalLargeNested 对比约 20–50 KB 大型嵌套实体的编码性能
func BenchmarkCodecMarshalLargeNested(b *testing.B) {
	benchmarkMarshalPaths(b, newLargeBenchmarkGoMessage(), newLargeBenchmarkProtoMessage(b))
}

// BenchmarkCodecUnmarshalLargeNested 对比约 20–50 KB 大型嵌套实体的解码性能
func BenchmarkCodecUnmarshalLargeNested(b *testing.B) {
	protoValue := newLargeBenchmarkProtoMessage(b)
	data, err := protojson.Marshal(protoValue)
	if err != nil {
		b.Fatal(err)
	}
	if len(data) < 20*1024 || len(data) > 50*1024 {
		b.Fatalf("大型嵌套基准 JSON 大小为 %d 字节，期望位于 20–50 KB", len(data))
	}
	descriptor := protoValue.Descriptor()
	benchmarkUnmarshalPaths(b, data,
		func() any { return &largeBenchmarkMessage{} },
		func() proto.Message { return dynamicpb.NewMessage(descriptor) },
	)
}

// BenchmarkCodecMarshalWide 对比固定 64 个标量字段的宽实体编码性能
func BenchmarkCodecMarshalWide(b *testing.B) {
	goType := newWideBenchmarkGoType()
	benchmarkMarshalPaths(b, newWideBenchmarkGoValue(goType).Interface(), newWideBenchmarkProtoMessage(b))
}

// BenchmarkCodecMarshalMapHeavy 对比包含多种 Map 键和大量成员的实体编码性能
func BenchmarkCodecMarshalMapHeavy(b *testing.B) {
	benchmarkMarshalPaths(b, newMapBenchmarkGoMessage(), newMapBenchmarkProtoMessage(b))
}

// BenchmarkCodecMarshalNumericDense 测量大量数字和布尔标量的编码性能
func BenchmarkCodecMarshalNumericDense(b *testing.B) {
	message := dynamicpb.NewMessage(numericBenchmarkDescriptor(b))
	descriptor := message.Descriptor()
	int32Values := message.Mutable(descriptor.Fields().ByName("int32_values")).List()
	int64Values := message.Mutable(descriptor.Fields().ByName("int64_values")).List()
	uint64Values := message.Mutable(descriptor.Fields().ByName("uint64_values")).List()
	doubleValues := message.Mutable(descriptor.Fields().ByName("double_values")).List()
	boolValues := message.Mutable(descriptor.Fields().ByName("bool_values")).List()
	for index := range numericBenchmarkItemCount {
		int32Values.Append(protoreflect.ValueOfInt32(int32(index - numericBenchmarkItemCount/2)))
		int64Values.Append(protoreflect.ValueOfInt64(int64(index+1) * 10000000001))
		uint64Values.Append(protoreflect.ValueOfUint64(uint64(index+1) * 10000000001))
		doubleValues.Append(protoreflect.ValueOfFloat64(float64(index)/10 + 0.25))
		boolValues.Append(protoreflect.ValueOfBool(index%2 == 0))
	}

	codec := NewCodec()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := codec.Marshal(message); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCodecMarshalBytesDense 测量较大普通 bytes 字段的 Base64 编码性能
func BenchmarkCodecMarshalBytesDense(b *testing.B) {
	message := newFeatureMessage(b)
	payload := make([]byte, bytesBenchmarkPayloadSize)
	for index := range payload {
		payload[index] = byte(index)
	}
	setField(message, "payload", protoreflect.ValueOfBytes(payload))
	codec := NewCodec()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := codec.Marshal(message); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCodecMarshalWKT 测量需要专用字符串格式化的 Well-Known Types
func BenchmarkCodecMarshalWKT(b *testing.B) {
	codec := NewCodec()
	benchmarks := []struct {
		name    string
		message proto.Message
	}{
		{name: "Timestamp", message: timestamppb.New(time.Date(2026, time.September, 12, 13, 14, 15, 123456789, time.UTC))},
		{name: "Duration", message: &durationpb.Duration{Seconds: -315576000000, Nanos: -123456789}},
		{name: "FieldMask", message: &fieldmaskpb.FieldMask{Paths: []string{"user_profile.display_name", "audit_entries.created_at", "nested.value"}}},
	}
	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := codec.Marshal(benchmark.message); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCodecUnmarshalWide 对比固定 64 个标量字段的宽实体解码性能
func BenchmarkCodecUnmarshalWide(b *testing.B) {
	goType := newWideBenchmarkGoType()
	protoValue := newWideBenchmarkProtoMessage(b)
	data, err := protojson.Marshal(protoValue)
	if err != nil {
		b.Fatal(err)
	}
	var members map[string]any
	if err := jsonv2.Unmarshal(data, &members); err != nil {
		b.Fatal(err)
	}
	if len(members) != wideBenchmarkFieldCount {
		b.Fatalf("宽实体 JSON 包含 %d 个字段，期望 %d 个", len(members), wideBenchmarkFieldCount)
	}
	for index := range wideBenchmarkFieldCount {
		name := fmt.Sprintf("field%02d", index+1)
		if _, ok := members[name]; !ok {
			b.Fatalf("宽实体 JSON 缺少字段 %q", name)
		}
	}
	descriptor := protoValue.Descriptor()
	benchmarkUnmarshalPaths(b, data,
		func() any { return reflect.New(goType).Interface() },
		func() proto.Message { return dynamicpb.NewMessage(descriptor) },
	)
}

// benchmarkMarshalPaths 使用相同语义数据运行五条编码路径，并按实际输出大小报告吞吐量
func benchmarkMarshalPaths(b *testing.B, goValue any, protoValue proto.Message) {
	codec := NewCodec()
	goJSON, err := jsonsonic.ConfigStd.Marshal(goValue)
	if err != nil {
		b.Fatal(err)
	}
	protoJSON, err := protojson.Marshal(protoValue)
	if err != nil {
		b.Fatal(err)
	}
	var goDocument any
	var protoDocument any
	if err := jsonv2.Unmarshal(goJSON, &goDocument); err != nil {
		b.Fatal(err)
	}
	if err := jsonv2.Unmarshal(protoJSON, &protoDocument); err != nil {
		b.Fatal(err)
	}
	if !reflect.DeepEqual(goDocument, protoDocument) {
		b.Fatal("普通 Go 与动态 Proto3 基准夹具的 JSON 语义不一致")
	}
	paths := []struct {
		name    string
		marshal func() ([]byte, error)
	}{
		{name: "NativeSonic", marshal: func() ([]byte, error) { return jsonsonic.ConfigStd.Marshal(goValue) }},
		{name: "NativeJSONV2", marshal: func() ([]byte, error) { return jsonv2.Marshal(goValue) }},
		{name: "GoStruct", marshal: func() ([]byte, error) { return codec.Marshal(goValue) }},
		{name: "NativeProtoJSON", marshal: func() ([]byte, error) { return protojson.Marshal(protoValue) }},
		{name: "Proto3", marshal: func() ([]byte, error) { return codec.Marshal(protoValue) }},
	}

	for _, path := range paths {
		b.Run(path.name, func(b *testing.B) {
			sample, err := path.marshal()
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(sample)))
			b.ResetTimer()
			for b.Loop() {
				if _, err := path.marshal(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// benchmarkUnmarshalPaths 使用同一份 ProtoJSON 输入运行五条解码路径
func benchmarkUnmarshalPaths(b *testing.B, data []byte, newGoTarget func() any, newProtoTarget func() proto.Message) {
	codec := NewCodec()
	paths := []struct {
		name      string
		unmarshal func() error
	}{
		{name: "NativeSonic", unmarshal: func() error { return jsonsonic.ConfigStd.Unmarshal(data, newGoTarget()) }},
		{name: "NativeJSONV2", unmarshal: func() error { return jsonv2.Unmarshal(data, newGoTarget()) }},
		{name: "GoStruct", unmarshal: func() error { return codec.Unmarshal(data, newGoTarget()) }},
		{name: "NativeProtoJSON", unmarshal: func() error { return protojson.Unmarshal(data, newProtoTarget()) }},
		{name: "Proto3", unmarshal: func() error { return codec.Unmarshal(data, newProtoTarget()) }},
	}

	for _, path := range paths {
		b.Run(path.name, func(b *testing.B) {
			if err := path.unmarshal(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for b.Loop() {
				if err := path.unmarshal(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// newLargeBenchmarkGoMessage 创建确定性的大型普通 Go 消息
func newLargeBenchmarkGoMessage() largeBenchmarkMessage {
	children := make([]largeBenchmarkChild, 0, largeBenchmarkChildCount)
	childMap := make(map[string]largeBenchmarkChild, 12)
	for index := range largeBenchmarkChildCount {
		child := newLargeBenchmarkChild(index)
		children = append(children, child)
		if index < 12 {
			childMap[fmt.Sprintf("child-%02d", index)] = child
		}
	}
	labels := make(map[string]string, 24)
	scores := make(map[string]int32, 24)
	for index := range 24 {
		key := fmt.Sprintf("metric-%02d", index)
		labels[key] = "stable-benchmark-label-" + strconv.Itoa(index)
		scores[key] = int32(index * 17)
	}
	return largeBenchmarkMessage{
		ID:         "large-message-01993b93-789a-7def-8123-456789abcdef",
		Title:      "大型 Proto3 JSON 编解码性能基准",
		Enabled:    true,
		TotalCount: 9223372036854775000,
		Children:   children,
		ChildMap:   childMap,
		Labels:     labels,
		Scores:     scores,
	}
}

// newLargeBenchmarkChild 创建指定序号的确定性子实体
func newLargeBenchmarkChild(index int) largeBenchmarkChild {
	tags := make([]string, 8)
	for tagIndex := range tags {
		tags[tagIndex] = fmt.Sprintf("child-%03d-tag-%02d-value", index, tagIndex)
	}
	numbers := make([]int32, 12)
	for numberIndex := range numbers {
		numbers[numberIndex] = int32(index*100 + numberIndex)
	}
	return largeBenchmarkChild{
		Sequence:    int32(index + 1),
		Total:       int64(index+1) * 10000000001,
		Name:        fmt.Sprintf("benchmark-child-%03d", index),
		Description: fmt.Sprintf("实体-%03d", index),
		Active:      index%3 != 0,
		Status:      "STATUS_ACTIVE",
		Numbers:     numbers,
		Tags:        tags,
	}
}

// newLargeBenchmarkProtoMessage 创建与大型普通 Go 消息语义一致的动态 Proto3 消息
func newLargeBenchmarkProtoMessage(b testing.TB) *dynamicpb.Message {
	b.Helper()
	descriptor := largeBenchmarkDescriptor(b)
	message := dynamicpb.NewMessage(descriptor)
	goValue := newLargeBenchmarkGoMessage()
	setField(message, "id", protoreflect.ValueOfString(goValue.ID))
	setField(message, "title", protoreflect.ValueOfString(goValue.Title))
	setField(message, "enabled", protoreflect.ValueOfBool(goValue.Enabled))
	setField(message, "total_count", protoreflect.ValueOfInt64(goValue.TotalCount))

	childrenField := descriptor.Fields().ByName("children")
	children := message.Mutable(childrenField).List()
	for _, child := range goValue.Children {
		children.Append(protoreflect.ValueOfMessage(newLargeBenchmarkProtoChild(childrenField.Message(), child)))
	}
	childMapField := descriptor.Fields().ByName("child_map")
	childMap := message.Mutable(childMapField).Map()
	for key, child := range goValue.ChildMap {
		childMap.Set(protoreflect.ValueOfString(key).MapKey(), protoreflect.ValueOfMessage(newLargeBenchmarkProtoChild(childMapField.MapValue().Message(), child)))
	}
	labelsField := descriptor.Fields().ByName("labels")
	labels := message.Mutable(labelsField).Map()
	for key, value := range goValue.Labels {
		labels.Set(protoreflect.ValueOfString(key).MapKey(), protoreflect.ValueOfString(value))
	}
	scoresField := descriptor.Fields().ByName("scores")
	scores := message.Mutable(scoresField).Map()
	for key, value := range goValue.Scores {
		scores.Set(protoreflect.ValueOfString(key).MapKey(), protoreflect.ValueOfInt32(value))
	}
	return message
}

// newLargeBenchmarkProtoChild 创建动态 Proto3 子实体
func newLargeBenchmarkProtoChild(descriptor protoreflect.MessageDescriptor, value largeBenchmarkChild) protoreflect.Message {
	message := dynamicpb.NewMessage(descriptor)
	setField(message, "sequence", protoreflect.ValueOfInt32(value.Sequence))
	setField(message, "total", protoreflect.ValueOfInt64(value.Total))
	setField(message, "name", protoreflect.ValueOfString(value.Name))
	setField(message, "description", protoreflect.ValueOfString(value.Description))
	setField(message, "active", protoreflect.ValueOfBool(value.Active))
	setField(message, "status", protoreflect.ValueOfEnum(1))
	numbers := message.Mutable(descriptor.Fields().ByName("numbers")).List()
	for _, number := range value.Numbers {
		numbers.Append(protoreflect.ValueOfInt32(number))
	}
	tags := message.Mutable(descriptor.Fields().ByName("tags")).List()
	for _, tag := range value.Tags {
		tags.Append(protoreflect.ValueOfString(tag))
	}
	return message
}

// largeBenchmarkDescriptor 创建大型嵌套基准使用的动态 Proto3 描述符
func largeBenchmarkDescriptor(b testing.TB) protoreflect.MessageDescriptor {
	b.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("sonic_large_benchmark.proto"),
		Package: proto.String("codec.benchmark.large"),
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("Status"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
				{Name: proto.String("STATUS_ACTIVE"), Number: proto.Int32(1)},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Child"), Field: []*descriptorpb.FieldDescriptorProto{
				protoField("sequence", 1, descriptorpb.FieldDescriptorProto_TYPE_INT32, ""),
				protoField("total", 2, descriptorpb.FieldDescriptorProto_TYPE_INT64, ""),
				protoField("name", 3, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				protoField("description", 4, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				protoField("active", 5, descriptorpb.FieldDescriptorProto_TYPE_BOOL, ""),
				protoField("status", 6, descriptorpb.FieldDescriptorProto_TYPE_ENUM, ".codec.benchmark.large.Status"),
				repeatedProtoField("numbers", 7, descriptorpb.FieldDescriptorProto_TYPE_INT32, ""),
				repeatedProtoField("tags", 8, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
			}},
			{Name: proto.String("Root"), NestedType: []*descriptorpb.DescriptorProto{
				mapEntry("ChildMapEntry", descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.benchmark.large.Child"),
				mapEntry("LabelsEntry", descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				mapEntry("ScoresEntry", descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_TYPE_INT32, ""),
			}, Field: []*descriptorpb.FieldDescriptorProto{
				protoField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				protoField("title", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				protoField("enabled", 3, descriptorpb.FieldDescriptorProto_TYPE_BOOL, ""),
				protoField("total_count", 4, descriptorpb.FieldDescriptorProto_TYPE_INT64, ""),
				repeatedProtoField("children", 5, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.benchmark.large.Child"),
				repeatedProtoField("child_map", 6, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.benchmark.large.Root.ChildMapEntry"),
				repeatedProtoField("labels", 7, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.benchmark.large.Root.LabelsEntry"),
				repeatedProtoField("scores", 8, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.benchmark.large.Root.ScoresEntry"),
			}},
		},
	}, nil)
	if err != nil {
		b.Fatalf("protodesc.NewFile() error = %v", err)
	}
	return file.Messages().ByName("Root")
}

// newWideBenchmarkGoType 创建带 JSON 标签的 64 字段运行时 Go 结构体类型
func newWideBenchmarkGoType() reflect.Type {
	fields := make([]reflect.StructField, wideBenchmarkFieldCount)
	for index := range fields {
		fieldNumber := index + 1
		jsonName := fmt.Sprintf("field%02d", fieldNumber)
		fieldType := reflect.TypeFor[int32]()
		tag := reflect.StructTag(`json:"` + jsonName + `"`)
		switch index % 4 {
		case 1:
			fieldType = reflect.TypeFor[int64]()
			tag = reflect.StructTag(`json:"` + jsonName + `,string"`)
		case 2:
			fieldType = reflect.TypeFor[string]()
		case 3:
			fieldType = reflect.TypeFor[bool]()
		}
		fields[index] = reflect.StructField{Name: fmt.Sprintf("Field%02d", fieldNumber), Type: fieldType, Tag: tag}
	}
	return reflect.StructOf(fields)
}

// newWideBenchmarkGoValue 创建所有字段均为非零值的宽实体
func newWideBenchmarkGoValue(messageType reflect.Type) reflect.Value {
	value := reflect.New(messageType).Elem()
	for index := range value.NumField() {
		field := value.Field(index)
		switch index % 4 {
		case 0:
			field.SetInt(int64(index + 101))
		case 1:
			field.SetInt(int64(index+1) * 10000000001)
		case 2:
			field.SetString(fmt.Sprintf("wide-field-%02d-stable-value", index+1))
		case 3:
			field.SetBool(true)
		}
	}
	return value
}

// newWideBenchmarkProtoMessage 创建与 64 字段普通 Go 宽实体语义一致的动态 Proto3 消息
func newWideBenchmarkProtoMessage(b testing.TB) *dynamicpb.Message {
	b.Helper()
	descriptor := wideBenchmarkDescriptor(b)
	message := dynamicpb.NewMessage(descriptor)
	for index := range wideBenchmarkFieldCount {
		field := descriptor.Fields().Get(index)
		switch index % 4 {
		case 0:
			message.Set(field, protoreflect.ValueOfInt32(int32(index+101)))
		case 1:
			message.Set(field, protoreflect.ValueOfInt64(int64(index+1)*10000000001))
		case 2:
			message.Set(field, protoreflect.ValueOfString(fmt.Sprintf("wide-field-%02d-stable-value", index+1)))
		case 3:
			message.Set(field, protoreflect.ValueOfBool(true))
		}
	}
	return message
}

// wideBenchmarkDescriptor 创建固定 64 个标量字段的动态 Proto3 描述符
func wideBenchmarkDescriptor(b testing.TB) protoreflect.MessageDescriptor {
	b.Helper()
	fields := make([]*descriptorpb.FieldDescriptorProto, wideBenchmarkFieldCount)
	for index := range fields {
		fieldType := descriptorpb.FieldDescriptorProto_TYPE_INT32
		switch index % 4 {
		case 1:
			fieldType = descriptorpb.FieldDescriptorProto_TYPE_INT64
		case 2:
			fieldType = descriptorpb.FieldDescriptorProto_TYPE_STRING
		case 3:
			fieldType = descriptorpb.FieldDescriptorProto_TYPE_BOOL
		}
		fields[index] = protoField(fmt.Sprintf("field_%02d", index+1), int32(index+1), fieldType, "")
	}
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("sonic_wide_benchmark.proto"),
		Package: proto.String("codec.benchmark.wide"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name:  proto.String("Wide"),
			Field: fields,
		}},
	}, nil)
	if err != nil {
		b.Fatalf("protodesc.NewFile() error = %v", err)
	}
	return file.Messages().ByName("Wide")
}

// numericBenchmarkDescriptor 创建数字密集基准使用的动态 Proto3 描述符
func numericBenchmarkDescriptor(b testing.TB) protoreflect.MessageDescriptor {
	b.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("sonic_numeric_benchmark.proto"),
		Package: proto.String("codec.benchmark.numeric"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Numbers"),
			Field: []*descriptorpb.FieldDescriptorProto{
				repeatedProtoField("int32_values", 1, descriptorpb.FieldDescriptorProto_TYPE_INT32, ""),
				repeatedProtoField("int64_values", 2, descriptorpb.FieldDescriptorProto_TYPE_INT64, ""),
				repeatedProtoField("uint64_values", 3, descriptorpb.FieldDescriptorProto_TYPE_UINT64, ""),
				repeatedProtoField("double_values", 4, descriptorpb.FieldDescriptorProto_TYPE_DOUBLE, ""),
				repeatedProtoField("bool_values", 5, descriptorpb.FieldDescriptorProto_TYPE_BOOL, ""),
			},
		}},
	}, nil)
	if err != nil {
		b.Fatalf("protodesc.NewFile() error = %v", err)
	}
	return file.Messages().ByName("Numbers")
}

// mapBenchmarkMessage 表示 Map 密集基准的普通 Go 消息
type mapBenchmarkMessage struct {
	StringValues   map[string]string `json:"stringValues"`
	SignedValues   map[int64]string  `json:"signedValues"`
	UnsignedValues map[uint64]string `json:"unsignedValues"`
}

// newMapBenchmarkGoMessage 创建多种键类型且包含固定数量成员的普通 Go 消息
func newMapBenchmarkGoMessage() mapBenchmarkMessage {
	message := mapBenchmarkMessage{
		StringValues:   make(map[string]string, mapBenchmarkEntryCount),
		SignedValues:   make(map[int64]string, mapBenchmarkEntryCount),
		UnsignedValues: make(map[uint64]string, mapBenchmarkEntryCount),
	}
	for index := range mapBenchmarkEntryCount {
		value := fmt.Sprintf("map-benchmark-value-%03d", index)
		message.StringValues[fmt.Sprintf("map-key-%03d", mapBenchmarkEntryCount-index)] = value
		message.SignedValues[int64(index)-mapBenchmarkEntryCount/2] = value
		message.UnsignedValues[uint64(mapBenchmarkEntryCount-index)] = value
	}
	return message
}

// newMapBenchmarkProtoMessage 创建与 Map 密集普通 Go 消息语义一致的动态 Proto3 消息
func newMapBenchmarkProtoMessage(b testing.TB) *dynamicpb.Message {
	b.Helper()
	descriptor := mapBenchmarkDescriptor(b)
	message := dynamicpb.NewMessage(descriptor)
	goValue := newMapBenchmarkGoMessage()

	stringValues := message.Mutable(descriptor.Fields().ByName("string_values")).Map()
	for key, value := range goValue.StringValues {
		stringValues.Set(protoreflect.ValueOfString(key).MapKey(), protoreflect.ValueOfString(value))
	}
	signedValues := message.Mutable(descriptor.Fields().ByName("signed_values")).Map()
	for key, value := range goValue.SignedValues {
		signedValues.Set(protoreflect.ValueOfInt64(key).MapKey(), protoreflect.ValueOfString(value))
	}
	unsignedValues := message.Mutable(descriptor.Fields().ByName("unsigned_values")).Map()
	for key, value := range goValue.UnsignedValues {
		unsignedValues.Set(protoreflect.ValueOfUint64(key).MapKey(), protoreflect.ValueOfString(value))
	}
	return message
}

// mapBenchmarkDescriptor 创建 Map 密集基准使用的动态 Proto3 描述符
func mapBenchmarkDescriptor(b testing.TB) protoreflect.MessageDescriptor {
	b.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("sonic_map_benchmark.proto"),
		Package: proto.String("codec.benchmark.mapheavy"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Maps"),
			NestedType: []*descriptorpb.DescriptorProto{
				mapEntry("StringValuesEntry", descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				mapEntry("SignedValuesEntry", descriptorpb.FieldDescriptorProto_TYPE_INT64, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				mapEntry("UnsignedValuesEntry", descriptorpb.FieldDescriptorProto_TYPE_UINT64, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
			},
			Field: []*descriptorpb.FieldDescriptorProto{
				repeatedProtoField("string_values", 1, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.benchmark.mapheavy.Maps.StringValuesEntry"),
				repeatedProtoField("signed_values", 2, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.benchmark.mapheavy.Maps.SignedValuesEntry"),
				repeatedProtoField("unsigned_values", 3, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.benchmark.mapheavy.Maps.UnsignedValuesEntry"),
			},
		}},
	}, nil)
	if err != nil {
		b.Fatalf("protodesc.NewFile() error = %v", err)
	}
	return file.Messages().ByName("Maps")
}
