//go:build go1.27

package json

import (
	"reflect"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v3/encoding"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	_ "google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type numericMessage struct {
	Int32    int32  `json:"int32"`
	Int64    int64  `json:"int64"`
	Int32Ptr *int32 `json:"int32_ptr"`
	Int64Ptr *int64 `json:"int64_ptr"`
}

type flexibleNames struct {
	PageSize int32 `json:"pageSize"`
}

type v2Marshaler struct{}

func (v2Marshaler) MarshalJSON() ([]byte, error) { return []byte(`"v2"`), nil }

type protoMarshaler struct {
	*dynamicpb.Message
}

func (protoMarshaler) MarshalJSON() ([]byte, error) { return []byte(`"wrong-dispatch"`), nil }

func TestCodec_MarshalOrdinaryValuesUsesJSONV2(t *testing.T) {
	got, err := NewCodec().Marshal(v2Marshaler{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(got) != `"v2"` {
		t.Fatalf("Marshal() = %s, want %q", got, `"v2"`)
	}
}

func TestCodec_MarshalProtoUsesProtoJSONNamesAndInt64Strings(t *testing.T) {
	message := newTestMessage(t)
	setInt32(message, "page_size", 20)
	setInt64(message, "total_count", 9223372036854775807)

	got, err := NewCodec().Marshal(message)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.ReplaceAll(string(got), " ", "") != `{"pageSize":20,"totalCount":"9223372036854775807"}` {
		t.Fatalf("Marshal() = %s", got)
	}
}

func TestCodec_MarshalPrefersProtoJSONOverJSONV2Marshaler(t *testing.T) {
	message := newTestMessage(t)
	setInt32(message, "page_size", 20)

	got, err := NewCodec().Marshal(protoMarshaler{Message: message})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.ReplaceAll(string(got), " ", "") != `{"pageSize":20}` {
		t.Fatalf("Marshal() = %s, want ProtoJSON", got)
	}
}

func TestCodec_UnmarshalNumericStrings(t *testing.T) {
	tests := []struct {
		name string
		data string
		want numericMessage
	}{
		{
			name: "numbers",
			data: `{"int32":42,"int64":9223372036854775807,"int32_ptr":-42,"int64_ptr":-9223372036854775808}`,
			want: numericMessage{Int32: 42, Int64: 9223372036854775807, Int32Ptr: int32Ptr(-42), Int64Ptr: int64Ptr(-9223372036854775808)},
		},
		{
			name: "numeric strings",
			data: `{"int32":"42","int64":"9223372036854775807","int32_ptr":"-42","int64_ptr":"-9223372036854775808"}`,
			want: numericMessage{Int32: 42, Int64: 9223372036854775807, Int32Ptr: int32Ptr(-42), Int64Ptr: int64Ptr(-9223372036854775808)},
		},
		{name: "empty strings clear pointers", data: `{"int32":0,"int64":0,"int32_ptr":"","int64_ptr":""}`, want: numericMessage{}},
		{name: "null clears pointers", data: `{"int32":0,"int64":0,"int32_ptr":null,"int64_ptr":null}`, want: numericMessage{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got numericMessage
			if err := NewCodec().Unmarshal([]byte(tt.data), &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			assertNumericMessage(t, got, tt.want)
		})
	}
}

func TestCodec_UnmarshalOrdinaryStructUsesFlexibleNames(t *testing.T) {
	tests := []string{`{"PAGESIZE":7}`, `{"page_size":7}`, `{"page-size":7}`}
	for _, data := range tests {
		t.Run(data, func(t *testing.T) {
			var got flexibleNames
			if err := NewCodec().Unmarshal([]byte(data), &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if got.PageSize != 7 {
				t.Fatalf("PageSize = %d, want 7", got.PageSize)
			}
		})
	}
}

func TestCodec_UnmarshalOrdinaryValuesUsesFlexibleNamesWithoutChangingMapKeys(t *testing.T) {
	var named flexibleNames
	if err := NewCodec().Unmarshal([]byte(`{"PAGE_SIZE":7}`), &named); err != nil {
		t.Fatalf("Unmarshal(struct) error = %v", err)
	}
	if named.PageSize != 7 {
		t.Fatalf("PageSize = %d, want 7", named.PageSize)
	}

	var mapped map[string]int
	if err := NewCodec().Unmarshal([]byte(`{"Page-Size":7}`), &mapped); err != nil {
		t.Fatalf("Unmarshal(map) error = %v", err)
	}
	if !reflect.DeepEqual(mapped, map[string]int{"Page-Size": 7}) {
		t.Fatalf("map = %#v", mapped)
	}
}

func TestCodec_UnmarshalOrdinaryValuesRejectsDuplicateAliases(t *testing.T) {
	var got flexibleNames
	if err := NewCodec().Unmarshal([]byte(`{"pageSize":1,"PAGE_SIZE":2}`), &got); err == nil {
		t.Fatal("Unmarshal() error = nil, want duplicate-name error")
	}
}

func TestCodec_UnmarshalProtoAcceptsAliasesAndPreservesOptionalPresence(t *testing.T) {
	tests := []string{"pageSize", "page_size", "PAGESIZE", "page-size", "PAGE_SIZE"}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			message := newTestMessage(t)
			data := `{"` + name + `":"12","optional-count":"9","unknown":true}`
			if err := NewCodec().Unmarshal([]byte(data), message); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			assertInt32(t, message, "page_size", 12)
			field := fieldByName(message, "optional_count")
			if !message.Has(field) || message.Get(field).Int() != 9 {
				t.Fatalf("optional_count = %v, presence = %v", message.Get(field), message.Has(field))
			}
		})
	}
}

func TestCodec_UnmarshalProtoFieldMatchPriority(t *testing.T) {
	message := newTestMessage(t)
	if err := NewCodec().Unmarshal([]byte(`{"specialName":1,"foobar":2}`), message); err != nil {
		t.Fatalf("Unmarshal(exact JSON names) error = %v", err)
	}
	assertInt32(t, message, "foo_bar", 1)
	assertInt32(t, message, "foobar", 2)

	message = newTestMessage(t)
	if err := NewCodec().Unmarshal([]byte(`{"foo_bar":3}`), message); err != nil {
		t.Fatalf("Unmarshal(exact proto name) error = %v", err)
	}
	assertInt32(t, message, "foo_bar", 3)
}

func TestCodec_UnmarshalProtoEmptyStringRules(t *testing.T) {
	message := newTestMessage(t)
	setInt32(message, "page_size", 3)
	setInt32(message, "optional_count", 4)
	if err := NewCodec().Unmarshal([]byte(`{"pageSize":"","optionalCount":"","wrappedCount":"","title":""}`), message); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if message.Has(fieldByName(message, "page_size")) || message.Has(fieldByName(message, "optional_count")) || message.Has(fieldByName(message, "wrapped_count")) {
		t.Fatal("numeric empty strings must be treated as fields not provided")
	}
	if got := message.Get(fieldByName(message, "title")).String(); got != "" {
		t.Fatalf("title = %q", got)
	}

	invalid := []string{
		`{"enabled":""}`,
		`{"status":""}`,
		`{"child":""}`,
		`{"numbers":[""]}`,
		`{"numberMap":{"key":""}}`,
	}
	for _, data := range invalid {
		t.Run(data, func(t *testing.T) {
			if err := NewCodec().Unmarshal([]byte(data), newTestMessage(t)); err == nil {
				t.Fatal("Unmarshal() error = nil, want non-nil")
			}
		})
	}
}

func TestCodec_UnmarshalProtoRecursesIntoNestedCollections(t *testing.T) {
	message := newTestMessage(t)
	data := `{"child":{"CHILD-COUNT":"1"},"children":[{"child_count":"2"}],"childMap":{"Keep-Key":{"ChildCount":"3"}}}`
	if err := NewCodec().Unmarshal([]byte(data), message); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	assertNestedInt32(t, message.Get(fieldByName(message, "child")).Message(), "child_count", 1)
	assertNestedInt32(t, message.Get(fieldByName(message, "children")).List().Get(0).Message(), "child_count", 2)
	childMap := message.Get(fieldByName(message, "child_map")).Map()
	value := childMap.Get(protoreflect.ValueOfString("Keep-Key").MapKey()).Message()
	assertNestedInt32(t, value, "child_count", 3)
}

func TestCodec_UnmarshalProtoRejectsConflictsDuplicatesAndTrailingData(t *testing.T) {
	tests := []string{
		`{"pageSize":1,"page_size":2}`,
		`{"pageSize":1,"pageSize":2}`,
		`{"fooBar":1}`,
		`{"pageSize":1} {}`,
	}
	for _, data := range tests {
		t.Run(data, func(t *testing.T) {
			if err := NewCodec().Unmarshal([]byte(data), newTestMessage(t)); err == nil {
				t.Fatal("Unmarshal() error = nil, want non-nil")
			}
		})
	}
}

func TestCodec_UnmarshalProtoValidatesRootSuffix(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{name: "trailing whitespace", data: "{\"pageSize\":1} \n\t"},
		{name: "second value", data: `{"pageSize":1} {}`, wantErr: true},
		{name: "incomplete object", data: `{"pageSize":1} {`, wantErr: true},
		{name: "invalid text", data: `{"pageSize":1} invalid`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewCodec().Unmarshal([]byte(tt.data), newTestMessage(t))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCodec_UnmarshalProtoSupportsDoublePointer(t *testing.T) {
	var target *wrapperspb.Int64Value
	if err := NewCodec().Unmarshal([]byte(`"8"`), &target); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if target == nil {
		t.Fatal("target = nil")
	}
	if target.Value != 8 {
		t.Fatalf("Value = %d, want 8", target.Value)
	}
}

func TestCodec_UnmarshalProtoReusesNonNilDoublePointer(t *testing.T) {
	target := &wrapperspb.Int64Value{Value: 1}
	original := target
	if err := NewCodec().Unmarshal([]byte(`"8"`), &target); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if target != original {
		t.Fatal("Unmarshal() replaced an existing **proto.Message target")
	}
	if target.Value != 8 {
		t.Fatalf("Value = %d, want 8", target.Value)
	}
}

func TestCodec_UnmarshalRejectsNilOuterDoublePointer(t *testing.T) {
	var target **wrapperspb.Int64Value
	if err := NewCodec().Unmarshal([]byte(`"8"`), target); err == nil {
		t.Fatal("Unmarshal() error = nil, want non-nil")
	}
}

func TestCodec_UnmarshalNonProtoDoublePointerUsesJSONV2(t *testing.T) {
	var target *numericMessage
	if err := NewCodec().Unmarshal([]byte(`{"int32":"8"}`), &target); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if target == nil || target.Int32 != 8 {
		t.Fatalf("target = %#v", target)
	}
}

func TestCodec_UnmarshalOrdinaryGoogleProtobufMessageUsesFlexibleFields(t *testing.T) {
	message := dynamicpb.NewMessage(ordinaryGoogleMessageDescriptor(t))
	if err := NewCodec().Unmarshal([]byte(`{"PAGE-SIZE":"7"}`), message); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	assertInt32(t, message, "page_size", 7)
}

func TestCodec_UnmarshalProtoProtectsWKTAndMapKeys(t *testing.T) {
	message := newTestMessage(t)
	data := `{"createdAt":"2026-09-07T06:55:48Z","metadata":{"Do-Not_Change":"value"}}`
	if err := NewCodec().Unmarshal([]byte(data), message); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	metadata := message.Get(fieldByName(message, "metadata")).Map()
	if got := metadata.Get(protoreflect.ValueOfString("Do-Not_Change").MapKey()).String(); got != "value" {
		t.Fatalf("metadata value = %q", got)
	}
	created, err := protojson.Marshal(message.Get(fieldByName(message, "created_at")).Message().Interface())
	if err != nil {
		t.Fatalf("protojson.Marshal(created_at) error = %v", err)
	}
	if string(created) != `"2026-09-07T06:55:48Z"` {
		t.Fatalf("created_at = %s", created)
	}
}

func TestCodec_UnmarshalNumericStringsRejectsInvalidValues(t *testing.T) {
	tests := []string{
		`{"int32":""}`, `{"int64":""}`, `{"int32":"invalid"}`, `{"int64_ptr":"invalid"}`,
		`{"int32":2147483648}`, `{"int32_ptr":"2147483648"}`, `{"int64":9223372036854775808}`,
		`{"int64_ptr":"9223372036854775808"}`, `{"int32":1.5}`, `{"int64_ptr":"1.5"}`,
	}
	for _, data := range tests {
		t.Run(data, func(t *testing.T) {
			var got numericMessage
			if err := NewCodec().Unmarshal([]byte(data), &got); err == nil {
				t.Fatal("Unmarshal() error = nil, want non-nil")
			}
		})
	}
}

func TestCodec_EmptyInputIsNoop(t *testing.T) {
	original := numericMessage{Int32: 1, Int64: 2, Int32Ptr: int32Ptr(3), Int64Ptr: int64Ptr(4)}
	got := original
	if err := NewCodec().Unmarshal(nil, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	assertNumericMessage(t, got, original)
}

func TestCodec_NameAndRegistration(t *testing.T) {
	Register()
	got := encoding.GetCodec(Name)
	if got == nil || got.Name() != Name {
		t.Fatalf("encoding.GetCodec(%q) = %#v", Name, got)
	}
	if reflect.TypeOf(got) != reflect.TypeOf(NewCodec()) {
		t.Fatalf("registered codec type = %T, want %T", got, NewCodec())
	}
}

func newTestMessage(t *testing.T) *dynamicpb.Message {
	t.Helper()
	return dynamicpb.NewMessage(testMessageDescriptor(t))
}

func testMessageDescriptor(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()
	optionalIndex := int32(0)
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:     proto.String("proto3"),
		Name:       proto.String("codec_test.proto"),
		Package:    proto.String("codec.test"),
		Dependency: []string{"google/protobuf/wrappers.proto", "google/protobuf/timestamp.proto"},
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("Status"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
				{Name: proto.String("STATUS_ACTIVE"), Number: proto.Int32(1)},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Child"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("child_count", 1, descriptorpb.FieldDescriptorProto_TYPE_INT32, ""),
					field("label", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				},
			},
			{
				Name:      proto.String("Root"),
				OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("_optional_count")}},
				NestedType: []*descriptorpb.DescriptorProto{
					mapEntry("ChildMapEntry", descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.test.Child"),
					mapEntry("NumberMapEntry", descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_TYPE_INT64, ""),
					mapEntry("MetadataEntry", descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				},
				Field: []*descriptorpb.FieldDescriptorProto{
					field("page_size", 1, descriptorpb.FieldDescriptorProto_TYPE_INT32, ""),
					field("total_count", 2, descriptorpb.FieldDescriptorProto_TYPE_INT64, ""),
					optionalField("optional_count", 3, descriptorpb.FieldDescriptorProto_TYPE_INT32, optionalIndex),
					field("title", 4, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
					field("enabled", 5, descriptorpb.FieldDescriptorProto_TYPE_BOOL, ""),
					field("status", 6, descriptorpb.FieldDescriptorProto_TYPE_ENUM, ".codec.test.Status"),
					field("child", 7, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.test.Child"),
					repeatedField("children", 8, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.test.Child"),
					repeatedField("child_map", 9, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.test.Root.ChildMapEntry"),
					repeatedField("number_map", 10, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.test.Root.NumberMapEntry"),
					field("wrapped_count", 11, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".google.protobuf.Int64Value"),
					repeatedField("numbers", 12, descriptorpb.FieldDescriptorProto_TYPE_INT32, ""),
					repeatedField("metadata", 13, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.test.Root.MetadataEntry"),
					field("created_at", 14, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".google.protobuf.Timestamp"),
					fieldWithJSONName("foo_bar", "specialName", 15, descriptorpb.FieldDescriptorProto_TYPE_INT32),
					field("foobar", 16, descriptorpb.FieldDescriptorProto_TYPE_INT32, ""),
				},
			},
		},
	}, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("protodesc.NewFile() error = %v", err)
	}
	return file.Messages().ByName("Root")
}

func ordinaryGoogleMessageDescriptor(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("ordinary_google_message_test.proto"),
		Package: proto.String("google.protobuf"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name:  proto.String("OrdinaryCodecMessage"),
			Field: []*descriptorpb.FieldDescriptorProto{field("page_size", 1, descriptorpb.FieldDescriptorProto_TYPE_INT32, "")},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("protodesc.NewFile() error = %v", err)
	}
	return file.Messages().ByName("OrdinaryCodecMessage")
}

func field(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type, typeName string) *descriptorpb.FieldDescriptorProto {
	f := &descriptorpb.FieldDescriptorProto{Name: proto.String(name), Number: proto.Int32(number), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: typ.Enum()}
	if typeName != "" {
		f.TypeName = proto.String(typeName)
	}
	return f
}

func fieldWithJSONName(name, jsonName string, number int32, typ descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	f := field(name, number, typ, "")
	f.JsonName = proto.String(jsonName)
	return f
}

func optionalField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type, oneofIndex int32) *descriptorpb.FieldDescriptorProto {
	f := field(name, number, typ, "")
	f.Proto3Optional = proto.Bool(true)
	f.OneofIndex = proto.Int32(oneofIndex)
	return f
}

func repeatedField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type, typeName string) *descriptorpb.FieldDescriptorProto {
	f := field(name, number, typ, typeName)
	f.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	return f
}

func mapEntry(name string, keyType, valueType descriptorpb.FieldDescriptorProto_Type, valueTypeName string) *descriptorpb.DescriptorProto {
	return &descriptorpb.DescriptorProto{
		Name:    proto.String(name),
		Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
		Field: []*descriptorpb.FieldDescriptorProto{
			field("key", 1, keyType, ""),
			field("value", 2, valueType, valueTypeName),
		},
	}
}

func fieldByName(message protoreflect.ProtoMessage, name protoreflect.Name) protoreflect.FieldDescriptor {
	return message.ProtoReflect().Descriptor().Fields().ByName(name)
}

func setInt32(message protoreflect.ProtoMessage, name protoreflect.Name, value int32) {
	message.ProtoReflect().Set(fieldByName(message, name), protoreflect.ValueOfInt32(value))
}

func setInt64(message protoreflect.ProtoMessage, name protoreflect.Name, value int64) {
	message.ProtoReflect().Set(fieldByName(message, name), protoreflect.ValueOfInt64(value))
}

func assertInt32(t *testing.T, message protoreflect.ProtoMessage, name protoreflect.Name, want int32) {
	t.Helper()
	if got := int32(message.ProtoReflect().Get(fieldByName(message, name)).Int()); got != want {
		t.Fatalf("%s = %d, want %d", name, got, want)
	}
}

func assertNestedInt32(t *testing.T, message protoreflect.Message, name protoreflect.Name, want int32) {
	t.Helper()
	field := message.Descriptor().Fields().ByName(name)
	if got := int32(message.Get(field).Int()); got != want {
		t.Fatalf("%s = %d, want %d", name, got, want)
	}
}

func int32Ptr(v int32) *int32 { return &v }
func int64Ptr(v int64) *int64 { return &v }

func assertNumericMessage(t *testing.T, got, want numericMessage) {
	t.Helper()
	if got.Int32 != want.Int32 || got.Int64 != want.Int64 || !sameInt32Ptr(got.Int32Ptr, want.Int32Ptr) || !sameInt64Ptr(got.Int64Ptr, want.Int64Ptr) {
		t.Fatalf("message = %+v, want %+v", got, want)
	}
}

func sameInt32Ptr(got, want *int32) bool {
	return got == nil && want == nil || got != nil && want != nil && *got == *want
}
func sameInt64Ptr(got, want *int64) bool {
	return got == nil && want == nil || got != nil && want != nil && *got == *want
}
