//go:build go1.27

package sonic

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"
	"uuid"

	uuidpb "github.com/azicen/kratos-extension/types/uuid"
	jsonsonic "github.com/bytedance/sonic"
	"github.com/go-kratos/kratos/v3/encoding"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type ordinaryValue struct {
	Name string `json:"name"`
}

type protoMarshaler struct {
	*dynamicpb.Message
}

func (protoMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`"wrong-dispatch"`), nil
}

func TestCodecMarshalOrdinaryValue(t *testing.T) {
	got, err := NewCodec().Marshal(ordinaryValue{Name: "sonic"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(got) != `{"name":"sonic"}` {
		t.Fatalf("Marshal() = %s", got)
	}
}

func TestCodecMarshalNilProto3MessageAsEmptyObject(t *testing.T) {
	var message *timestamppb.Timestamp
	got, err := NewCodec().Marshal(message)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(got) != `{}` {
		t.Fatalf("Marshal() = %s", got)
	}
}

func TestCodecMarshalProto3UsesProtoJSONRepresentation(t *testing.T) {
	message := newRootMessage(t)
	setField(message, "page_size", protoreflect.ValueOfInt32(20))
	setField(message, "total_count", protoreflect.ValueOfInt64(9223372036854775807))
	setField(message, "status", protoreflect.ValueOfEnum(1))

	got, err := NewCodec().Marshal(protoMarshaler{Message: message})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `{"pageSize":20,"totalCount":"9223372036854775807","status":"STATUS_ACTIVE"}`
	if string(got) != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}

func TestCodecRoundTripsNestedUUIDWithoutBase64JSON(t *testing.T) {
	wantUUID := uuid.MustParse("01993b93-789a-7def-8123-456789abcdef")
	message := dynamicpb.NewMessage(uuidContainerDescriptor(t))
	idField := message.Descriptor().Fields().ByName("id")
	message.Set(idField, protoreflect.ValueOfMessage(uuidpb.Wrap(wantUUID).ProtoReflect()))

	encoded, err := NewCodec().Marshal(message)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(encoded) != `{"id":"01993b93-789a-7def-8123-456789abcdef"}` {
		t.Fatalf("Marshal() = %s", encoded)
	}

	decoded := dynamicpb.NewMessage(uuidContainerDescriptor(t))
	if err := NewCodec().Unmarshal(encoded, decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	gotBytes := decoded.Get(decoded.Descriptor().Fields().ByName("id")).Message().Get(uuidValueField()).Bytes()
	if got := uuid.UUID(gotBytes); got != wantUUID {
		t.Fatalf("decoded UUID = %s, want %s", got, wantUUID)
	}
}

func TestCodecRoundTripsTimestamp(t *testing.T) {
	want := timestamppb.New(time.Unix(0, 0).UTC())
	encoded, err := NewCodec().Marshal(want)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(encoded) != `"1970-01-01T00:00:00Z"` {
		t.Fatalf("Marshal() = %s", encoded)
	}

	var got timestamppb.Timestamp
	if err := NewCodec().Unmarshal(encoded, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !proto.Equal(&got, want) {
		t.Fatalf("Unmarshal() = %v, want %v", &got, want)
	}
}

func TestCodecMarshalTimestampFractionalPrecision(t *testing.T) {
	tests := []struct {
		name  string
		nanos int32
		want  string
	}{
		{name: "zero", nanos: 0, want: `"1970-01-01T00:00:00Z"`},
		{name: "millisecond", nanos: 123000000, want: `"1970-01-01T00:00:00.123Z"`},
		{name: "microsecond", nanos: 123456000, want: `"1970-01-01T00:00:00.123456Z"`},
		{name: "nanosecond", nanos: 123456789, want: `"1970-01-01T00:00:00.123456789Z"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := NewCodec().Marshal(&timestamppb.Timestamp{Nanos: tt.nanos})
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(encoded) != tt.want {
				t.Fatalf("Marshal() = %s, want %s", encoded, tt.want)
			}
		})
	}
}

func TestCodecRoundTripsDurationAndWrapperNull(t *testing.T) {
	want := &durationpb.Duration{Seconds: -1, Nanos: -500000000}
	encoded, err := NewCodec().Marshal(want)
	if err != nil {
		t.Fatalf("Marshal(Duration) error = %v", err)
	}
	if string(encoded) != `"-1.500s"` {
		t.Fatalf("Marshal(Duration) = %s", encoded)
	}
	var decoded durationpb.Duration
	if err := NewCodec().Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal(Duration) error = %v", err)
	}
	if !proto.Equal(&decoded, want) {
		t.Fatalf("Duration round trip = %v, want %v", &decoded, want)
	}

	wrapper := &wrapperspb.Int64Value{Value: 7}
	if err := NewCodec().Unmarshal([]byte("null"), wrapper); err == nil {
		t.Fatal("Unmarshal(wrapper null) error = nil")
	}
}

func TestCodecMarshalDurationFractionalPrecision(t *testing.T) {
	tests := []struct {
		name     string
		seconds  int64
		nanos    int32
		wantJSON string
	}{
		{name: "zero", wantJSON: `"0s"`},
		{name: "whole seconds", seconds: 12, wantJSON: `"12s"`},
		{name: "milliseconds", seconds: 1, nanos: 123000000, wantJSON: `"1.123s"`},
		{name: "microseconds", seconds: 1, nanos: 123456000, wantJSON: `"1.123456s"`},
		{name: "nanoseconds", seconds: 1, nanos: 123456789, wantJSON: `"1.123456789s"`},
		{name: "negative subsecond", nanos: -1, wantJSON: `"-0.000000001s"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := NewCodec().Marshal(&durationpb.Duration{Seconds: tt.seconds, Nanos: tt.nanos})
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(encoded) != tt.wantJSON {
				t.Fatalf("Marshal() = %s, want %s", encoded, tt.wantJSON)
			}
		})
	}
}

func TestCodecRejectsUnknownProto3Field(t *testing.T) {
	if err := NewCodec().Unmarshal([]byte(`{"unknown":true}`), newRootMessage(t)); err == nil {
		t.Fatal("Unmarshal() error = nil, want unknown-field error")
	}
}

func TestCodecUnmarshalProto3CollectionsPresenceAndOneof(t *testing.T) {
	message := newFeatureMessage(t)
	data := []byte(`{"optionalCount":"7","name":"alice","numbers":[1,"2"],"labels":{"":3,"x":"4"},"payload":"AQID"}`)
	if err := NewCodec().Unmarshal(data, message); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	optional := message.Descriptor().Fields().ByName("optional_count")
	if !message.Has(optional) || message.Get(optional).Int() != 7 {
		t.Fatalf("optional_count = %v, presence = %v", message.Get(optional), message.Has(optional))
	}
	name := message.Descriptor().Fields().ByName("name")
	if !message.Has(name) || message.Get(name).String() != "alice" {
		t.Fatalf("name = %q, presence = %v", message.Get(name).String(), message.Has(name))
	}
	numbers := message.Get(message.Descriptor().Fields().ByName("numbers")).List()
	if numbers.Len() != 2 || numbers.Get(0).Int() != 1 || numbers.Get(1).Int() != 2 {
		t.Fatalf("numbers = %v", numbers)
	}
	labels := message.Get(message.Descriptor().Fields().ByName("labels")).Map()
	if labels.Get(protoreflect.ValueOfString("").MapKey()).Int() != 3 || labels.Get(protoreflect.ValueOfString("x").MapKey()).Int() != 4 {
		t.Fatal("labels were not decoded")
	}
	if got := message.Get(message.Descriptor().Fields().ByName("payload")).Bytes(); !reflect.DeepEqual(got, []byte{1, 2, 3}) {
		t.Fatalf("payload = %v", got)
	}

	encoded, err := NewCodec().Marshal(message)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	decoded := dynamicpb.NewMessage(message.Descriptor())
	if err := NewCodec().Unmarshal(encoded, decoded); err != nil {
		t.Fatalf("Unmarshal(Marshal(message)) error = %v", err)
	}
	if !proto.Equal(decoded, message) {
		t.Fatalf("Unmarshal(Marshal(message)) = %v, want %v", decoded, message)
	}
}

func TestCodecMarshalProto3Options(t *testing.T) {
	message := newRootMessage(t)
	setField(message, "page_size", protoreflect.ValueOfInt32(20))
	setField(message, "status", protoreflect.ValueOfEnum(1))

	tests := []struct {
		name    string
		options Options
		message proto.Message
		want    string
	}{
		{name: "proto field names and enum numbers", options: Options{UseProtoNames: true, UseEnumNumbers: true}, message: message, want: `{"page_size":20,"status":1}`},
		{name: "default values", options: Options{EmitDefaultValues: true}, message: newRootMessage(t), want: `{"pageSize":0,"totalCount":"0","status":"STATUS_UNSPECIFIED"}`},
		{name: "unpopulated message presence", options: Options{EmitUnpopulated: true}, message: dynamicpb.NewMessage(uuidContainerDescriptor(t)), want: `{"id":null}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewCodec(tt.options).Marshal(tt.message)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCodecMarshalProto3ScalarRepresentations(t *testing.T) {
	message := newFeatureMessage(t)
	setField(message, "payload", protoreflect.ValueOfBytes([]byte{0xfb, 0xff, 0x00}))
	encoded, err := NewCodec().Marshal(message)
	if err != nil {
		t.Fatalf("Marshal(bytes) error = %v", err)
	}
	if string(encoded) != `{"payload":"+/8A"}` {
		t.Fatalf("Marshal(bytes) = %s", encoded)
	}

	floatTests := []struct {
		name  string
		value float64
		want  string
	}{
		{name: "NaN", value: math.NaN(), want: `"NaN"`},
		{name: "positive infinity", value: math.Inf(1), want: `"Infinity"`},
		{name: "negative infinity", value: math.Inf(-1), want: `"-Infinity"`},
	}
	for _, tt := range floatTests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewCodec().Marshal(&wrapperspb.DoubleValue{Value: tt.value})
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCodecMarshalEveryProto3ScalarRepresentation(t *testing.T) {
	message := dynamicpb.NewMessage(scalarDescriptor(t))
	descriptor := message.Descriptor()
	message.Set(descriptor.Fields().ByName("enabled"), protoreflect.ValueOfBool(true))
	message.Set(descriptor.Fields().ByName("signed_32"), protoreflect.ValueOfInt32(-2147483648))
	message.Set(descriptor.Fields().ByName("unsigned_32"), protoreflect.ValueOfUint32(4294967295))
	message.Set(descriptor.Fields().ByName("signed_64"), protoreflect.ValueOfInt64(-9223372036854775808))
	message.Set(descriptor.Fields().ByName("unsigned_64"), protoreflect.ValueOfUint64(18446744073709551615))
	message.Set(descriptor.Fields().ByName("float_value"), protoreflect.ValueOfFloat32(1.25))
	message.Set(descriptor.Fields().ByName("double_value"), protoreflect.ValueOfFloat64(-2.5))
	message.Set(descriptor.Fields().ByName("payload"), protoreflect.ValueOfBytes([]byte{0x00, 0x01, 0xfe, 0xff}))

	encoded, err := NewCodec().Marshal(message)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `{"enabled":true,"signed32":-2147483648,"unsigned32":4294967295,"signed64":"-9223372036854775808","unsigned64":"18446744073709551615","floatValue":1.25,"doubleValue":-2.5,"payload":"AAH+/w=="}`
	if string(encoded) != want {
		t.Fatalf("Marshal() = %s, want %s", encoded, want)
	}
}

func TestCodecMarshalFloatUsesProtoJSONNumberFormat(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		want  string
	}{
		{name: "fixed lower boundary", value: 1e-6, want: `0.000001`},
		{name: "exponent below lower boundary", value: 1e-7, want: `1e-7`},
		{name: "fixed upper boundary", value: 1e20, want: `100000000000000000000`},
		{name: "exponent at upper boundary", value: 1e21, want: `1e+21`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := NewCodec().Marshal(&wrapperspb.DoubleValue{Value: tt.value})
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(encoded) != tt.want {
				t.Fatalf("Marshal() = %s, want %s", encoded, tt.want)
			}
		})
	}
}

func TestCodecRoundTripsEveryProto3MapKeyKind(t *testing.T) {
	message := dynamicpb.NewMessage(mapKeyDescriptor(t))
	setStringMapValues(message, "string_values", []mapStringValue{{key: protoreflect.ValueOfString("quote\"").MapKey(), value: "string"}})
	setStringMapValues(message, "bool_values", []mapStringValue{
		{key: protoreflect.ValueOfBool(false).MapKey(), value: "false"},
		{key: protoreflect.ValueOfBool(true).MapKey(), value: "true"},
	})
	setStringMapValues(message, "signed_values", []mapStringValue{
		{key: protoreflect.ValueOfInt64(-7).MapKey(), value: "negative"},
		{key: protoreflect.ValueOfInt64(9).MapKey(), value: "positive"},
	})
	setStringMapValues(message, "unsigned_values", []mapStringValue{{key: protoreflect.ValueOfUint64(7).MapKey(), value: "seven"}})

	for iteration := range 20 {
		encoded, err := NewCodec().Marshal(message)
		if err != nil {
			t.Fatalf("Marshal() iteration %d error = %v", iteration, err)
		}
		var document map[string]any
		if err := jsonv2.Unmarshal(encoded, &document); err != nil {
			t.Fatalf("Unmarshal JSON object iteration %d error = %v", iteration, err)
		}
		if len(document) != 4 {
			t.Fatalf("JSON object iteration %d contains %d fields, want 4", iteration, len(document))
		}

		decoded := dynamicpb.NewMessage(message.Descriptor())
		if err := NewCodec().Unmarshal(encoded, decoded); err != nil {
			t.Fatalf("Unmarshal() iteration %d error = %v", iteration, err)
		}
		if !proto.Equal(decoded, message) {
			t.Fatalf("Unmarshal(Marshal(message)) iteration %d = %v, want %v", iteration, decoded, message)
		}
	}
}

func TestCodecRoundTripsOrdinaryMessageInsideAny(t *testing.T) {
	descriptor := rootDescriptor(t)
	messageType := dynamicpb.NewMessageType(descriptor)
	resolver := new(protoregistry.Types)
	if err := resolver.RegisterMessage(messageType); err != nil {
		t.Fatal(err)
	}
	embedded := messageType.New().Interface()
	reflected := embedded.ProtoReflect()
	reflected.Set(descriptor.Fields().ByName("page_size"), protoreflect.ValueOfInt32(20))
	reflected.Set(descriptor.Fields().ByName("status"), protoreflect.ValueOfEnum(1))
	raw, err := proto.Marshal(embedded)
	if err != nil {
		t.Fatal(err)
	}
	message := &anypb.Any{TypeUrl: "type.googleapis.com/codec.test.Root", Value: raw}
	codec := NewCodec(Options{Resolver: resolver})

	encoded, err := codec.Marshal(message)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(encoded) != `{"@type":"type.googleapis.com/codec.test.Root","pageSize":20,"status":"STATUS_ACTIVE"}` {
		t.Fatalf("Marshal() = %s", encoded)
	}
	var decoded anypb.Any
	if err := codec.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded.TypeUrl != message.TypeUrl {
		t.Fatalf("Unmarshal(Marshal(message)) type URL = %q, want %q", decoded.TypeUrl, message.TypeUrl)
	}
	decodedEmbedded := messageType.New().Interface()
	if err := proto.Unmarshal(decoded.Value, decodedEmbedded); err != nil {
		t.Fatalf("Unmarshal embedded message error = %v", err)
	}
	if !proto.Equal(decodedEmbedded, embedded) {
		t.Fatalf("Unmarshal(Marshal(message)) embedded message = %v, want %v", decodedEmbedded, embedded)
	}
}

func TestCodecKeepsMetadataSeparateForSameFullNameDescriptors(t *testing.T) {
	firstDescriptor := sameNameDescriptor(t, "firstName")
	secondDescriptor := sameNameDescriptor(t, "secondName")
	first := dynamicpb.NewMessage(firstDescriptor)
	second := dynamicpb.NewMessage(secondDescriptor)
	first.Set(firstDescriptor.Fields().Get(0), protoreflect.ValueOfInt32(1))
	second.Set(secondDescriptor.Fields().Get(0), protoreflect.ValueOfInt32(2))
	codec := NewCodec()

	firstJSON, err := codec.Marshal(first)
	if err != nil {
		t.Fatalf("Marshal(first) error = %v", err)
	}
	secondJSON, err := codec.Marshal(second)
	if err != nil {
		t.Fatalf("Marshal(second) error = %v", err)
	}
	if string(firstJSON) != `{"firstName":1}` {
		t.Fatalf("Marshal(first) = %s", firstJSON)
	}
	if string(secondJSON) != `{"secondName":2}` {
		t.Fatalf("Marshal(second) = %s", secondJSON)
	}
}

func TestCodecMarshalMetadataIsConcurrentSafe(t *testing.T) {
	message := newRootMessage(t)
	setField(message, "page_size", protoreflect.ValueOfInt32(20))
	setField(message, "status", protoreflect.ValueOfEnum(1))
	codec := NewCodec()

	const workerCount = 32
	var waitGroup sync.WaitGroup
	errors := make(chan error, workerCount)
	for range workerCount {
		waitGroup.Go(func() {
			encoded, err := codec.Marshal(message)
			if err != nil {
				errors <- err
				return
			}
			if string(encoded) != `{"pageSize":20,"status":"STATUS_ACTIVE"}` {
				errors <- fmt.Errorf("Marshal() = %s", encoded)
			}
		})
	}
	waitGroup.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestCodecMarshalEnumAliasUsesFirstDeclaredName(t *testing.T) {
	descriptor := enumAliasDescriptor(t)
	message := dynamicpb.NewMessage(descriptor)
	message.Set(descriptor.Fields().ByName("status"), protoreflect.ValueOfEnum(1))

	encoded, err := NewCodec().Marshal(message)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(encoded) != `{"status":"STATUS_ACTIVE"}` {
		t.Fatalf("Marshal() = %s", encoded)
	}
}

func TestDescriptorMetadataCacheHasFixedCapacity(t *testing.T) {
	cache := new(descriptorMetadataCache)
	codec := codec{options: Options{RecursionLimit: 100}, json: jsonsonic.ConfigStd, metadata: cache}
	for index := range maxCachedDescriptors + 1 {
		descriptor := sameNameDescriptor(t, fmt.Sprintf("field%d", index))
		message := dynamicpb.NewMessage(descriptor)
		message.Set(descriptor.Fields().Get(0), protoreflect.ValueOfInt32(1))
		if _, err := codec.Marshal(message); err != nil {
			t.Fatalf("Marshal(%d) error = %v", index, err)
		}
	}
	if got := cache.messageCount.Load(); got != maxCachedDescriptors {
		t.Fatalf("message cache count = %d, want %d", got, maxCachedDescriptors)
	}
}

func TestCodecUnmarshalKeepsMetadataSeparateForSameFullNameDescriptors(t *testing.T) {
	firstDescriptor := sameNameDescriptor(t, "firstName")
	secondDescriptor := sameNameDescriptor(t, "secondName")
	codec := NewCodec()

	first := dynamicpb.NewMessage(firstDescriptor)
	if err := codec.Unmarshal([]byte(`{"firstName":1}`), first); err != nil {
		t.Fatalf("Unmarshal(first) error = %v", err)
	}
	second := dynamicpb.NewMessage(secondDescriptor)
	if err := codec.Unmarshal([]byte(`{"secondName":2}`), second); err != nil {
		t.Fatalf("Unmarshal(second) error = %v", err)
	}
	if got := first.Get(firstDescriptor.Fields().Get(0)).Int(); got != 1 {
		t.Fatalf("first field = %d, want 1", got)
	}
	if got := second.Get(secondDescriptor.Fields().Get(0)).Int(); got != 2 {
		t.Fatalf("second field = %d, want 2", got)
	}
}

func TestCodecUnmarshalMetadataIsConcurrentSafe(t *testing.T) {
	descriptor := rootDescriptor(t)
	codec := NewCodec()
	const workerCount = 32
	var waitGroup sync.WaitGroup
	errors := make(chan error, workerCount)
	for range workerCount {
		waitGroup.Go(func() {
			message := dynamicpb.NewMessage(descriptor)
			if err := codec.Unmarshal([]byte(`{"pageSize":20,"status":"STATUS_ACTIVE"}`), message); err != nil {
				errors <- err
				return
			}
			if got := message.Get(descriptor.Fields().ByName("page_size")).Int(); got != 20 {
				errors <- fmt.Errorf("page_size = %d", got)
			}
		})
	}
	waitGroup.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestCodecUnmarshalReusesDescriptorMetadata(t *testing.T) {
	descriptor := rootDescriptor(t)
	registered := NewCodec().(codec)
	for range 2 {
		message := dynamicpb.NewMessage(descriptor)
		if err := registered.Unmarshal([]byte(`{"pageSize":20}`), message); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
	}
	if got := registered.metadata.messageCount.Load(); got != 1 {
		t.Fatalf("message cache count = %d, want 1", got)
	}
}

func TestCodecRejectsDuplicateFieldsAndOneof(t *testing.T) {
	tests := []string{
		`{"optionalCount":1,"optional_count":2}`,
		`{"name":"alice","code":7}`,
		`{"labels":{"x":1,"x":2}}`,
	}
	for _, data := range tests {
		t.Run(data, func(t *testing.T) {
			if err := NewCodec().Unmarshal([]byte(data), newFeatureMessage(t)); err == nil {
				t.Fatal("Unmarshal() error = nil, want conflict error")
			}
		})
	}
}

func TestCodecAllocatesProto3DoublePointer(t *testing.T) {
	var target *timestamppb.Timestamp
	if err := NewCodec().Unmarshal([]byte(`"1970-01-01T00:00:00Z"`), &target); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if target == nil || target.Seconds != 0 || target.Nanos != 0 {
		t.Fatalf("target = %#v", target)
	}
}

func TestCodecDoesNotAllocateProto3DoublePointerOnFailure(t *testing.T) {
	var target *timestamppb.Timestamp
	if err := NewCodec().Unmarshal([]byte(`"invalid"`), &target); err == nil {
		t.Fatal("Unmarshal() error = nil")
	}
	if target != nil {
		t.Fatalf("target = %#v, want nil", target)
	}
}

func TestCodecUnmarshalProto3NumericAndNullRules(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{name: "int32 number", data: `{"optionalCount":7}`},
		{name: "int32 string", data: `{"optionalCount":"7"}`},
		{name: "int32 exponent", data: `{"optionalCount":1e2}`},
		{name: "int32 zero fraction string", data: `{"optionalCount":"7.0"}`},
		{name: "null leaves field unset", data: `{"optionalCount":null}`},
		{name: "empty numeric string", data: `{"optionalCount":""}`, wantErr: true},
		{name: "overflow", data: `{"optionalCount":2147483648}`, wantErr: true},
		{name: "fraction", data: `{"optionalCount":1.5}`, wantErr: true},
		{name: "invalid numeric string", data: `{"optionalCount":"+7"}`, wantErr: true},
		{name: "loose alias rejected", data: `{"OPTIONAL-COUNT":7}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := newFeatureMessage(t)
			err := NewCodec().Unmarshal([]byte(tt.data), message)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCodecCanDiscardUnknownProto3Fields(t *testing.T) {
	codec := NewCodec(Options{DiscardUnknown: true})
	message := newRootMessage(t)
	if err := codec.Unmarshal([]byte(`{"pageSize":7,"unknown":{"nested":true}}`), message); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got := message.Get(message.Descriptor().Fields().ByName("page_size")).Int(); got != 7 {
		t.Fatalf("page_size = %d", got)
	}
}

func TestCodecRoundTripsStructValueAndFieldMask(t *testing.T) {
	structure, err := structpb.NewStruct(map[string]any{
		"name":  "alice",
		"count": 2.0,
		"items": []any{true, nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := NewCodec().Marshal(structure)
	if err != nil {
		t.Fatalf("Marshal(Struct) error = %v", err)
	}
	var decoded structpb.Struct
	if err := NewCodec().Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal(Struct) error = %v", err)
	}
	if !proto.Equal(&decoded, structure) {
		t.Fatalf("Struct round trip = %v, want %v", &decoded, structure)
	}

	mask := &fieldmaskpb.FieldMask{Paths: []string{"foo_bar", "nested.value"}}
	encoded, err = NewCodec().Marshal(mask)
	if err != nil {
		t.Fatalf("Marshal(FieldMask) error = %v", err)
	}
	if string(encoded) != `"fooBar,nested.value"` {
		t.Fatalf("Marshal(FieldMask) = %s", encoded)
	}
	var decodedMask fieldmaskpb.FieldMask
	if err := NewCodec().Unmarshal(encoded, &decodedMask); err != nil {
		t.Fatalf("Unmarshal(FieldMask) error = %v", err)
	}
	if !proto.Equal(&decodedMask, mask) {
		t.Fatalf("FieldMask round trip = %v, want %v", &decodedMask, mask)
	}
}

func TestCodecRoundTripsAny(t *testing.T) {
	embedded := timestamppb.New(time.Unix(0, 123000000).UTC())
	message, err := anypb.New(embedded)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := NewCodec().Marshal(message)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `{"@type":"type.googleapis.com/google.protobuf.Timestamp","value":"1970-01-01T00:00:00.123Z"}`
	if string(encoded) != want {
		t.Fatalf("Marshal() = %s, want %s", encoded, want)
	}

	var decoded anypb.Any
	if err := NewCodec().Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !proto.Equal(&decoded, message) {
		t.Fatalf("Any round trip = %v, want %v", &decoded, message)
	}
}

func TestCodecAnyEmptyAndStrictInput(t *testing.T) {
	embedded, err := anypb.New(&emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := NewCodec().Marshal(embedded)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `{"@type":"type.googleapis.com/google.protobuf.Empty","value":{}}`
	if string(encoded) != want {
		t.Fatalf("Marshal() = %s, want %s", encoded, want)
	}
	if err := NewCodec().Unmarshal([]byte(`{"@type":"type.googleapis.com/google.protobuf.Empty","value":{},"extra":1}`), &anypb.Any{}); err == nil {
		t.Fatal("Unmarshal(Any unknown field) error = nil")
	}
	if err := NewCodec().Unmarshal([]byte{0x22, 0xff, 0x22}, newRootMessage(t)); err == nil {
		t.Fatal("Unmarshal(invalid UTF-8) error = nil")
	}
}

func TestCodecRejectsNullMapValue(t *testing.T) {
	if err := NewCodec().Unmarshal([]byte(`{"labels":{"x":null}}`), newFeatureMessage(t)); err == nil {
		t.Fatal("Unmarshal() error = nil")
	}
}

func TestCodecRegistration(t *testing.T) {
	Register()
	got := encoding.GetCodec(Name)
	if got == nil || got.Name() != Name {
		t.Fatalf("GetCodec(%q) = %#v", Name, got)
	}
	if reflect.TypeOf(got) != reflect.TypeOf(NewCodec()) {
		t.Fatalf("registered codec type = %T, want %T", got, NewCodec())
	}
}

func newRootMessage(t testing.TB) *dynamicpb.Message {
	t.Helper()
	return dynamicpb.NewMessage(rootDescriptor(t))
}

func rootDescriptor(t testing.TB) protoreflect.MessageDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("sonic_codec_test.proto"),
		Package: proto.String("codec.test"),
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("Status"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
				{Name: proto.String("STATUS_ACTIVE"), Number: proto.Int32(1)},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Root"),
			Field: []*descriptorpb.FieldDescriptorProto{
				protoField("page_size", 1, descriptorpb.FieldDescriptorProto_TYPE_INT32, ""),
				protoField("total_count", 2, descriptorpb.FieldDescriptorProto_TYPE_INT64, ""),
				protoField("status", 3, descriptorpb.FieldDescriptorProto_TYPE_ENUM, ".codec.test.Status"),
			},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("protodesc.NewFile() error = %v", err)
	}
	return file.Messages().ByName("Root")
}

func uuidContainerDescriptor(t testing.TB) protoreflect.MessageDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:     proto.String("proto3"),
		Name:       proto.String("sonic_uuid_container_test.proto"),
		Package:    proto.String("codec.uuidtest"),
		Dependency: []string{"types/uuid/uuid.proto"},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Container"),
			Field: []*descriptorpb.FieldDescriptorProto{
				protoField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".uuid.UUID"),
			},
		}},
	}, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("protodesc.NewFile() error = %v", err)
	}
	return file.Messages().ByName("Container")
}

func newFeatureMessage(t testing.TB) *dynamicpb.Message {
	t.Helper()
	return dynamicpb.NewMessage(featureDescriptor(t))
}

func featureDescriptor(t testing.TB) protoreflect.MessageDescriptor {
	t.Helper()
	choiceIndex := int32(0)
	optionalIndex := int32(1)
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("sonic_feature_test.proto"),
		Package: proto.String("codec.feature"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Feature"),
			OneofDecl: []*descriptorpb.OneofDescriptorProto{
				{Name: proto.String("choice")},
				{Name: proto.String("_optional_count")},
			},
			NestedType: []*descriptorpb.DescriptorProto{
				mapEntry("LabelsEntry", descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_TYPE_INT64, ""),
			},
			Field: []*descriptorpb.FieldDescriptorProto{
				optionalProtoField("optional_count", 1, descriptorpb.FieldDescriptorProto_TYPE_INT32, optionalIndex),
				oneofProtoField("name", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, choiceIndex),
				oneofProtoField("code", 3, descriptorpb.FieldDescriptorProto_TYPE_INT32, choiceIndex),
				repeatedProtoField("numbers", 4, descriptorpb.FieldDescriptorProto_TYPE_INT32, ""),
				repeatedProtoField("labels", 5, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.feature.Feature.LabelsEntry"),
				protoField("payload", 6, descriptorpb.FieldDescriptorProto_TYPE_BYTES, ""),
			},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("protodesc.NewFile() error = %v", err)
	}
	return file.Messages().ByName("Feature")
}

func scalarDescriptor(t testing.TB) protoreflect.MessageDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("sonic_scalar_test.proto"),
		Package: proto.String("codec.scalar"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Scalars"),
			Field: []*descriptorpb.FieldDescriptorProto{
				protoField("enabled", 1, descriptorpb.FieldDescriptorProto_TYPE_BOOL, ""),
				protoField("signed_32", 2, descriptorpb.FieldDescriptorProto_TYPE_INT32, ""),
				protoField("unsigned_32", 3, descriptorpb.FieldDescriptorProto_TYPE_UINT32, ""),
				protoField("signed_64", 4, descriptorpb.FieldDescriptorProto_TYPE_INT64, ""),
				protoField("unsigned_64", 5, descriptorpb.FieldDescriptorProto_TYPE_UINT64, ""),
				protoField("float_value", 6, descriptorpb.FieldDescriptorProto_TYPE_FLOAT, ""),
				protoField("double_value", 7, descriptorpb.FieldDescriptorProto_TYPE_DOUBLE, ""),
				protoField("payload", 8, descriptorpb.FieldDescriptorProto_TYPE_BYTES, ""),
			},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("protodesc.NewFile() error = %v", err)
	}
	return file.Messages().ByName("Scalars")
}

func mapKeyDescriptor(t testing.TB) protoreflect.MessageDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("sonic_map_key_test.proto"),
		Package: proto.String("codec.mapkey"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Maps"),
			NestedType: []*descriptorpb.DescriptorProto{
				mapEntry("StringValuesEntry", descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				mapEntry("BoolValuesEntry", descriptorpb.FieldDescriptorProto_TYPE_BOOL, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				mapEntry("SignedValuesEntry", descriptorpb.FieldDescriptorProto_TYPE_INT64, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
				mapEntry("UnsignedValuesEntry", descriptorpb.FieldDescriptorProto_TYPE_UINT64, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
			},
			Field: []*descriptorpb.FieldDescriptorProto{
				repeatedProtoField("string_values", 1, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.mapkey.Maps.StringValuesEntry"),
				repeatedProtoField("bool_values", 2, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.mapkey.Maps.BoolValuesEntry"),
				repeatedProtoField("signed_values", 3, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.mapkey.Maps.SignedValuesEntry"),
				repeatedProtoField("unsigned_values", 4, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".codec.mapkey.Maps.UnsignedValuesEntry"),
			},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("protodesc.NewFile() error = %v", err)
	}
	return file.Messages().ByName("Maps")
}

func sameNameDescriptor(t testing.TB, jsonName string) protoreflect.MessageDescriptor {
	t.Helper()
	field := protoField("value", 1, descriptorpb.FieldDescriptorProto_TYPE_INT32, "")
	field.JsonName = proto.String(jsonName)
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String(jsonName + ".proto"),
		Package: proto.String("codec.metadata"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name:  proto.String("SameName"),
			Field: []*descriptorpb.FieldDescriptorProto{field},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("protodesc.NewFile() error = %v", err)
	}
	return file.Messages().ByName("SameName")
}

func enumAliasDescriptor(t testing.TB) protoreflect.MessageDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("sonic_enum_alias_test.proto"),
		Package: proto.String("codec.enumalias"),
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name:    proto.String("Status"),
			Options: &descriptorpb.EnumOptions{AllowAlias: proto.Bool(true)},
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
				{Name: proto.String("STATUS_ACTIVE"), Number: proto.Int32(1)},
				{Name: proto.String("STATUS_ENABLED"), Number: proto.Int32(1)},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Root"),
			Field: []*descriptorpb.FieldDescriptorProto{
				protoField("status", 1, descriptorpb.FieldDescriptorProto_TYPE_ENUM, ".codec.enumalias.Status"),
			},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("protodesc.NewFile() error = %v", err)
	}
	return file.Messages().ByName("Root")
}

type mapStringValue struct {
	key   protoreflect.MapKey
	value string
}

func setStringMapValues(message *dynamicpb.Message, name protoreflect.Name, values []mapStringValue) {
	mapping := message.Mutable(message.Descriptor().Fields().ByName(name)).Map()
	for _, value := range values {
		mapping.Set(value.key, protoreflect.ValueOfString(value.value))
	}
}

func protoField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type, typeName string) *descriptorpb.FieldDescriptorProto {
	field := &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(number),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Type:   typ.Enum(),
	}
	if typeName != "" {
		field.TypeName = proto.String(typeName)
	}
	return field
}

func optionalProtoField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type, oneofIndex int32) *descriptorpb.FieldDescriptorProto {
	field := protoField(name, number, typ, "")
	field.Proto3Optional = proto.Bool(true)
	field.OneofIndex = proto.Int32(oneofIndex)
	return field
}

func oneofProtoField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type, oneofIndex int32) *descriptorpb.FieldDescriptorProto {
	field := protoField(name, number, typ, "")
	field.OneofIndex = proto.Int32(oneofIndex)
	return field
}

func repeatedProtoField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type, typeName string) *descriptorpb.FieldDescriptorProto {
	field := protoField(name, number, typ, typeName)
	field.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	return field
}

func mapEntry(name string, keyType, valueType descriptorpb.FieldDescriptorProto_Type, valueTypeName string) *descriptorpb.DescriptorProto {
	return &descriptorpb.DescriptorProto{
		Name:    proto.String(name),
		Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
		Field: []*descriptorpb.FieldDescriptorProto{
			protoField("key", 1, keyType, ""),
			protoField("value", 2, valueType, valueTypeName),
		},
	}
}

func setField(message *dynamicpb.Message, name protoreflect.Name, value protoreflect.Value) {
	message.Set(message.Descriptor().Fields().ByName(name), value)
}

func uuidValueField() protoreflect.FieldDescriptor {
	return uuidpb.File_types_uuid_uuid_proto.Messages().ByName("UUID").Fields().ByName("data")
}
