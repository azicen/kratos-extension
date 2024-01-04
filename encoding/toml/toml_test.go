package toml

import (
	"reflect"
	"testing"
	"time"
)

func TestCodec_Unmarshal(t *testing.T) {
	tests := []struct {
		data  string
		value interface{}
	}{
		{
			"",
			(*struct{})(nil),
		},
		{
			"# This is a TOML comment",
			(*struct{})(nil),
		},
		{
			"v = true",
			map[string]interface{}{"v": true},
		},
		{
			"v = +99",
			map[string]interface{}{"v": 99},
		},
		{
			"v = 42",
			map[string]interface{}{"v": 42},
		},
		{
			"v = 0",
			map[string]interface{}{"v": 0},
		},
		{
			"v = -17",
			map[string]interface{}{"v": -17},
		},
		{
			"v = 0xDEADBEEF",
			map[string]interface{}{"v": 3735928559},
		},
		{
			"v = 0xdeadbeef",
			map[string]interface{}{"v": 3735928559},
		},
		{
			"v = 0xdead_beef",
			map[string]interface{}{"v": 3735928559},
		},
		{
			"v = 0o01234567",
			map[string]interface{}{"v": 342391},
		},
		{
			"v = 0o755",
			map[string]interface{}{"v": 493},
		},
		{
			"v = 0b11010110",
			map[string]interface{}{"v": 214},
		},
		{
			"v = +1.0",
			map[string]interface{}{"v": 1.0},
		},
		{
			"v = 3.1415",
			map[string]interface{}{"v": 3.1415},
		},
		{
			"v = -0.01",
			map[string]interface{}{"v": -0.01},
		},
		{
			"v = 224_617.445_991_228",
			map[string]interface{}{"v": 224617.445991228},
		},
		{
			`v = "I'm a string"`,
			map[string]interface{}{"v": "I'm a string"},
		},
		{
			`v = """Roses are red
Violets are blue"""`,
			map[string]interface{}{"v": "Roses are red\nViolets are blue"},
		},
		{
			`v = '<\i\c*\s*>'`,
			map[string]interface{}{"v": "<\\i\\c*\\s*>"},
		},
		{
			"v = 1979-05-27T07:32:00Z",
			map[string]interface{}{"v": time.Date(1979, time.April, 5, 27, 7, 32, 0, time.UTC)},
		},
		{
			"v = [\"apple\", \"banana\"]",
			map[string]interface{}{"v": []string{"apple", "banana"}},
		},
	}

	for _, tt := range tests {
		v := reflect.ValueOf(tt.value).Type()
		value := reflect.New(v)
		err := (codec{}).Unmarshal([]byte(tt.data), value.Interface())
		if err != nil {
			t.Fatalf("(codec{}).Unmarshal should not return err: %v", err)
		}
	}
}

func TestCodec_Marshal(t *testing.T) {
	value := map[string]string{"v": "hi"}
	got, err := (codec{}).Marshal(value)
	if err != nil {
		t.Fatalf("should not return err")
	}
	if string(got) != "v = 'hi'\n" {
		t.Fatalf("want v = 'hi' return %s", string(got))
	}
}
