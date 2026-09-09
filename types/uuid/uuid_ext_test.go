package uuid

import (
	"encoding/json"
	"testing"

	googleuuid "github.com/google/uuid"
)

func TestUUIDMarshalJSON(t *testing.T) {
	valid := googleuuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	tests := []struct {
		name    string
		value   *UUID
		want    string
		wantErr bool
	}{
		{name: "empty", value: &UUID{}, want: `""`},
		{name: "valid", value: Wrap(valid), want: `"550e8400-e29b-41d4-a716-446655440000"`},
		{name: "invalid bytes", value: &UUID{Value: []byte{1}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("json.Marshal() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && string(got) != tt.want {
				t.Fatalf("json.Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestUUIDUnmarshalJSON(t *testing.T) {
	original := googleuuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	want := googleuuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	tests := []struct {
		name     string
		data     string
		want     googleuuid.UUID
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
				t.Fatalf("json.Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
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
	want := googleuuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	data, err := json.Marshal(Wrap(want))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got UUID
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got.Unwrap() != want {
		t.Fatalf("UUID = %s, want %s", got.Unwrap(), want)
	}
}
