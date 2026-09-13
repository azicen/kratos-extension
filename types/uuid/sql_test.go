package uuid

import (
	"database/sql/driver"
	"testing"

	"github.com/google/uuid"
)

func TestUUIDValue(t *testing.T) {
	valid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	tests := []struct {
		name    string
		value   *UUID
		want    driver.Value
		wantErr bool
	}{
		{name: "nil receiver", value: nil, want: nil},
		{name: "empty", value: &UUID{}, want: nil},
		{name: "valid", value: Wrap(valid), want: "550e8400-e29b-41d4-a716-446655440000"},
		{name: "invalid bytes", value: &UUID{Data: []byte{1}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.value.Value()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Value() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("Value() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUUIDScan(t *testing.T) {
	want := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	tests := []struct {
		name    string
		src     any
		want    uuid.UUID
		wantNil bool
		wantErr bool
	}{
		{name: "nil", src: nil, wantNil: true},
		{name: "string", src: "550e8400-e29b-41d4-a716-446655440000", want: want},
		{name: "bytes string", src: []byte("550e8400-e29b-41d4-a716-446655440000"), want: want},
		{name: "bytes binary", src: []byte(want[:]), want: want},
		{name: "empty string", src: "", wantNil: true},
		{name: "empty bytes", src: []byte{}, wantNil: true},
		{name: "array", src: want, want: want},
		{name: "invalid string", src: "invalid", wantErr: true},
		{name: "invalid type", src: 123, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &UUID{}
			err := got.Scan(tt.src)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Scan() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantNil {
				if len(got.Data) != 0 {
					t.Fatalf("Scan() = %v, want empty", got.Data)
				}
				return
			}
			if !tt.wantErr && got.Unwrap() != tt.want {
				t.Fatalf("Scan() = %s, want %s", got.Unwrap(), tt.want)
			}
		})
	}
}

func TestUUIDScanPreservesOnInvalidInput(t *testing.T) {
	original := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	target := Wrap(original)
	if err := target.Scan("invalid"); err == nil {
		t.Fatal("Scan() error = nil, want error")
	}
	if target.Unwrap() != original {
		t.Fatalf("Scan() = %s, want preserved value %s", target.Unwrap(), original)
	}
}

func TestUUIDSQLRoundTrip(t *testing.T) {
	want := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")

	value, err := Wrap(want).Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}

	var got UUID
	if err := got.Scan(value); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if got.Unwrap() != want {
		t.Fatalf("UUID = %s, want %s", got.Unwrap(), want)
	}
}
