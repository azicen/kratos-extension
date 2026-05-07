package log

import (
	"context"
	"log/slog"
	"testing"
	"time"

	klog "github.com/go-kratos/kratos/v2/log"
)

// mockLogger captures the most-recent call to Log for assertion.
type mockLogger struct {
	level   klog.Level
	keyvals []any
}

func (m *mockLogger) Log(level klog.Level, keyvals ...interface{}) error {
	m.level = level
	m.keyvals = make([]any, len(keyvals))
	copy(m.keyvals, keyvals)
	return nil
}

// findVal returns the value paired with key in a flat keyvals slice.
func findVal(keyvals []any, key string) (any, bool) {
	for i := 0; i+1 < len(keyvals); i += 2 {
		if k, ok := keyvals[i].(string); ok && k == key {
			return keyvals[i+1], true
		}
	}
	return nil, false
}

func TestEnabled_DefaultLevel(t *testing.T) {
	h := NewKratosHandler(&mockLogger{})

	tests := []struct {
		level slog.Level
		want  bool
	}{
		{slog.LevelDebug, false},
		{slog.LevelInfo - 1, false},
		{slog.LevelInfo, true},
		{slog.LevelWarn, true},
		{slog.LevelError, true},
	}
	for _, tc := range tests {
		got := h.Enabled(context.Background(), tc.level)
		if got != tc.want {
			t.Errorf("Enabled(%v) = %v, want %v", tc.level, got, tc.want)
		}
	}
}

func TestEnabled_WithLevel(t *testing.T) {
	h := NewKratosHandler(&mockLogger{}, WithLevel(slog.LevelDebug))
	if !h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected Debug to be enabled when WithLevel(LevelDebug)")
	}
}

func TestHandle_Message(t *testing.T) {
	mock := &mockLogger{}
	h := NewKratosHandler(mock)

	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "hello", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	val, ok := findVal(mock.keyvals, "msg")
	if !ok || val != "hello" {
		t.Errorf("expected msg=hello in keyvals %v", mock.keyvals)
	}
}

func TestHandle_LevelMapping(t *testing.T) {
	tests := []struct {
		slogLevel   slog.Level
		kratosLevel klog.Level
	}{
		{slog.LevelDebug, klog.LevelDebug},
		{slog.LevelInfo, klog.LevelInfo},
		{slog.LevelWarn, klog.LevelWarn},
		{slog.LevelError, klog.LevelError},
		{slog.LevelError + 4, klog.LevelError},
	}
	for _, tc := range tests {
		mock := &mockLogger{}
		h := NewKratosHandler(mock, WithLevel(slog.LevelDebug))
		r := slog.NewRecord(time.Time{}, tc.slogLevel, "test", 0)
		if err := h.Handle(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		if mock.level != tc.kratosLevel {
			t.Errorf("slog level %v: expected kratos %v, got %v",
				tc.slogLevel, tc.kratosLevel, mock.level)
		}
	}
}

func TestHandle_Attrs(t *testing.T) {
	mock := &mockLogger{}
	h := NewKratosHandler(mock)

	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "test", 0)
	r.AddAttrs(slog.String("key1", "val1"), slog.Int("key2", 42))
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	if val, ok := findVal(mock.keyvals, "key1"); !ok || val != "val1" {
		t.Errorf("expected key1=val1, keyvals=%v", mock.keyvals)
	}
	if val, ok := findVal(mock.keyvals, "key2"); !ok || val != int64(42) {
		t.Errorf("expected key2=42 (int64), got %v (%T), keyvals=%v", val, val, mock.keyvals)
	}
}

func TestHandle_GroupAttr(t *testing.T) {
	mock := &mockLogger{}
	h := NewKratosHandler(mock)

	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "request", 0)
	r.AddAttrs(slog.Group("http",
		slog.String("method", "POST"),
		slog.Int("status", 200),
	))
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	if val, ok := findVal(mock.keyvals, "http.method"); !ok || val != "POST" {
		t.Errorf("expected http.method=POST in keyvals %v", mock.keyvals)
	}
	if _, ok := findVal(mock.keyvals, "http.status"); !ok {
		t.Errorf("expected http.status in keyvals %v", mock.keyvals)
	}
}

func TestHandle_InlineGroup(t *testing.T) {
	mock := &mockLogger{}
	h := NewKratosHandler(mock)

	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "test", 0)
	// Group with empty key: attrs should be inlined (no prefix)
	r.AddAttrs(slog.Group("", slog.String("inlined", "yes")))
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	if val, ok := findVal(mock.keyvals, "inlined"); !ok || val != "yes" {
		t.Errorf("expected inlined=yes, keyvals=%v", mock.keyvals)
	}
}

func TestWithAttrs(t *testing.T) {
	mock := &mockLogger{}
	h := NewKratosHandler(mock).WithAttrs([]slog.Attr{slog.String("service", "api")})

	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "request", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	if val, ok := findVal(mock.keyvals, "service"); !ok || val != "api" {
		t.Errorf("expected service=api in keyvals %v", mock.keyvals)
	}
}

func TestWithAttrs_DoesNotMutateParent(t *testing.T) {
	mock := &mockLogger{}
	base := NewKratosHandler(mock)
	base.WithAttrs([]slog.Attr{slog.String("extra", "value")})

	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "test", 0)
	if err := base.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	if _, ok := findVal(mock.keyvals, "extra"); ok {
		t.Error("WithAttrs must not mutate the original handler")
	}
}

func TestWithAttrs_Empty(t *testing.T) {
	mock := &mockLogger{}
	h := NewKratosHandler(mock)
	h2 := h.WithAttrs(nil)
	if h2 != h {
		t.Error("WithAttrs(nil) should return the same handler")
	}
}

func TestWithGroup(t *testing.T) {
	mock := &mockLogger{}
	h := NewKratosHandler(mock).WithGroup("request")

	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "test", 0)
	r.AddAttrs(slog.String("method", "GET"))
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	if val, ok := findVal(mock.keyvals, "request.method"); !ok || val != "GET" {
		t.Errorf("expected request.method=GET in keyvals %v", mock.keyvals)
	}
}

func TestWithGroup_EmptyName(t *testing.T) {
	mock := &mockLogger{}
	h := NewKratosHandler(mock)
	h2 := h.WithGroup("")

	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "test", 0)
	r.AddAttrs(slog.String("key", "value"))
	if err := h2.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	// No group prefix should be applied
	if _, ok := findVal(mock.keyvals, "key"); !ok {
		t.Errorf("WithGroup(\"\") must not add a prefix, keyvals=%v", mock.keyvals)
	}
}

func TestWithGroup_WithAttrs_Combined(t *testing.T) {
	mock := &mockLogger{}
	h := NewKratosHandler(mock).
		WithGroup("server").
		WithAttrs([]slog.Attr{slog.String("host", "localhost")})

	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "start", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	if val, ok := findVal(mock.keyvals, "server.host"); !ok || val != "localhost" {
		t.Errorf("expected server.host=localhost in keyvals %v", mock.keyvals)
	}
}

func TestWithGroup_Nested(t *testing.T) {
	mock := &mockLogger{}
	h := NewKratosHandler(mock).WithGroup("a").WithGroup("b")

	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "test", 0)
	r.AddAttrs(slog.String("k", "v"))
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	if val, ok := findVal(mock.keyvals, "a.b.k"); !ok || val != "v" {
		t.Errorf("expected a.b.k=v in keyvals %v", mock.keyvals)
	}
}
