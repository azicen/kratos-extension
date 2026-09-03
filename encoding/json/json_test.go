//go:build go1.27

package json

import (
	"testing"

	"github.com/go-kratos/kratos/v3/encoding"
)

type numericMessage struct {
	Int32    int32  `json:"int32"`
	Int64    int64  `json:"int64"`
	Int32Ptr *int32 `json:"int32_ptr"`
	Int64Ptr *int64 `json:"int64_ptr"`
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
			want: numericMessage{
				Int32:    42,
				Int64:    9223372036854775807,
				Int32Ptr: int32Ptr(-42),
				Int64Ptr: int64Ptr(-9223372036854775808),
			},
		},
		{
			name: "numeric strings",
			data: `{"int32":"42","int64":"9223372036854775807","int32_ptr":"-42","int64_ptr":"-9223372036854775808"}`,
			want: numericMessage{
				Int32:    42,
				Int64:    9223372036854775807,
				Int32Ptr: int32Ptr(-42),
				Int64Ptr: int64Ptr(-9223372036854775808),
			},
		},
		{
			name: "empty strings clear pointers",
			data: `{"int32":0,"int64":0,"int32_ptr":"","int64_ptr":""}`,
			want: numericMessage{},
		},
		{
			name: "null clears pointers",
			data: `{"int32":0,"int64":0,"int32_ptr":null,"int64_ptr":null}`,
			want: numericMessage{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got numericMessage
			if err := (codec{}).Unmarshal([]byte(tt.data), &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			assertNumericMessage(t, got, tt.want)
		})
	}
}

func TestCodec_UnmarshalNumericStringsRejectsInvalidValues(t *testing.T) {
	tests := []string{
		`{"int32":""}`,
		`{"int64":""}`,
		`{"int32":"invalid"}`,
		`{"int64_ptr":"invalid"}`,
		`{"int32":2147483648}`,
		`{"int32_ptr":"2147483648"}`,
		`{"int64":9223372036854775808}`,
		`{"int64_ptr":"9223372036854775808"}`,
		`{"int32":1.5}`,
		`{"int64_ptr":"1.5"}`,
	}

	for _, data := range tests {
		t.Run(data, func(t *testing.T) {
			var got numericMessage
			if err := (codec{}).Unmarshal([]byte(data), &got); err == nil {
				t.Fatal("Unmarshal() error = nil, want non-nil")
			}
		})
	}
}

func TestCodec_EmptyInputIsNoop(t *testing.T) {
	original := numericMessage{Int32: 1, Int64: 2, Int32Ptr: int32Ptr(3), Int64Ptr: int64Ptr(4)}
	got := original

	if err := (codec{}).Unmarshal(nil, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	assertNumericMessage(t, got, original)
}

func TestCodec_NameAndRegistration(t *testing.T) {
	if got := (codec{}).Name(); got != Name {
		t.Fatalf("Name() = %q, want %q", got, Name)
	}
	if got := encoding.GetCodec(Name); got == nil {
		t.Fatalf("encoding.GetCodec(%q) = nil", Name)
	}
}

func int32Ptr(v int32) *int32 { return &v }

func int64Ptr(v int64) *int64 { return &v }

func assertNumericMessage(t *testing.T, got, want numericMessage) {
	t.Helper()
	if got.Int32 != want.Int32 || got.Int64 != want.Int64 {
		t.Fatalf("values = %+v, want %+v", got, want)
	}
	if !sameInt32Ptr(got.Int32Ptr, want.Int32Ptr) || !sameInt64Ptr(got.Int64Ptr, want.Int64Ptr) {
		t.Fatalf("pointers = %+v, want %+v", got, want)
	}
}

func sameInt32Ptr(got, want *int32) bool {
	return got == nil && want == nil || got != nil && want != nil && *got == *want
}

func sameInt64Ptr(got, want *int64) bool {
	return got == nil && want == nil || got != nil && want != nil && *got == *want
}
