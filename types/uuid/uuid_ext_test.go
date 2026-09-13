package uuid

import (
	"encoding/json/v2"
	"testing"
	"uuid"
)

var (
	_ json.Marshaler       = (*UUID)(nil)
	_ json.Unmarshaler     = (*UUID)(nil)
	_ json.MarshalerTo     = (*UUID)(nil)
	_ json.UnmarshalerFrom = (*UUID)(nil)
)

func TestUUIDMarshalJSON(t *testing.T) {
	valid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	tests := []struct {
		name    string
		value   *UUID
		want    string
		wantErr bool
	}{
		{name: "empty", value: &UUID{}, want: `""`},
		{name: "valid", value: Wrap(valid), want: `"550e8400-e29b-41d4-a716-446655440000"`},
		{name: "invalid bytes", value: &UUID{Data: []byte{1}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("jsonv2.Marshal() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && string(got) != tt.want {
				t.Fatalf("jsonv2.Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestUUIDUnmarshalJSON(t *testing.T) {
	original := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	want := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	tests := []struct {
		name     string
		data     string
		want     uuid.UUID
		wantErr  bool
		preserve bool
	}{
		{name: "empty preserves value", data: `""`, preserve: true},
		{name: "valid", data: `"550e8400-e29b-41d4-a716-446655440000"`, want: want},
		{name: "invalid UUID", data: `"invalid"`, wantErr: true, preserve: true},
		{name: "null", data: `null`, wantErr: true, preserve: true},
		{name: "non-string", data: `123`, wantErr: true, preserve: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := Wrap(original)
			err := json.Unmarshal([]byte(tt.data), target)
			if (err != nil) != tt.wantErr {
				t.Fatalf("jsonv2.Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.preserve {
				if got := target.Unwrap(); got != original {
					t.Fatalf("UUID = %s, want preserved value %s", got, original)
				}
				return
			}
			if got := target.Unwrap(); got != tt.want {
				t.Fatalf("UUID = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestUUIDJSONRoundTrip(t *testing.T) {
	want := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	data, err := json.Marshal(Wrap(want))
	if err != nil {
		t.Fatalf("jsonv2.Marshal() error = %v", err)
	}

	var got UUID
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("jsonv2.Unmarshal() error = %v", err)
	}
	if got.Unwrap() != want {
		t.Fatalf("UUID = %s, want %s", got.Unwrap(), want)
	}
}

func TestUUIDJSONV2RoundTrip(t *testing.T) {
	want := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	data, err := json.Marshal(Wrap(want))
	if err != nil {
		t.Fatalf("jsonv2.Marshal() error = %v", err)
	}
	if got := string(data); got != `"550e8400-e29b-41d4-a716-446655440000"` {
		t.Fatalf("jsonv2.Marshal() = %s", got)
	}

	var got UUID
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("jsonv2.Unmarshal() error = %v", err)
	}
	if got.Unwrap() != want {
		t.Fatalf("UUID = %s, want %s", got.Unwrap(), want)
	}
}

func TestUUIDUnmarshalJSONV2RejectsNonString(t *testing.T) {
	for _, data := range []string{"null", "123", "true", `{}`} {
		t.Run(data, func(t *testing.T) {
			var got UUID
			if err := json.Unmarshal([]byte(data), &got); err == nil {
				t.Fatalf("jsonv2.Unmarshal(%s) error = nil", data)
			}
		})
	}
}
