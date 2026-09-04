package log

import (
	"bytes"
	"context"
	"log/slog"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestColorHandlerColorsPrimaryFieldsAndSupportsGroups(t *testing.T) {
	var output bytes.Buffer
	handler := NewColorHandler(&output, WithLevel(slog.LevelDebug))
	logger := slog.New(handler).With("service.name", "energy-common").WithGroup("request").With("id", "req-1")

	logger.LogAttrs(context.Background(), slog.LevelInfo, "query completed",
		slog.String("trace.id", "trace-1"),
		slog.String("trace_id", "trace-id-1"),
		slog.String("span_id", "span-id-1"),
		slog.String("sql", "SELECT 1"),
		slog.String("elapsed", "12ms"),
		slog.Int("rows", 1),
		slog.String("error", "connection reset"),
	)

	got := output.String()
	assertFieldColored(t, got, "time", "", ansiGray)
	assertFieldColored(t, got, "level", "INFO", ansiGreen)
	assertFieldColored(t, got, "msg", `"query completed"`, ansiGreen)
	assertContains(t, got, ansiGray+"service.name=energy-common"+ansiReset)
	assertContains(t, got, "request.id=req-1")
	assertContains(t, got, ansiPurple+"request.trace.id=trace-1"+ansiReset)
	assertFieldColored(t, got, "request.trace_id", "trace-id-1", ansiPurple)
	assertFieldColored(t, got, "request.span_id", "span-id-1", ansiPurple)
	assertFieldColored(t, got, "request.sql", `"SELECT 1"`, ansiCyan)
	assertFieldColored(t, got, "request.elapsed", "12ms", ansiGreen)
	assertFieldColored(t, got, "request.rows", "1", ansiBlue)
	assertContains(t, got, ansiRed+`request.error="connection reset"`+ansiReset)

	basic := stripANSI(got)
	for _, want := range []string{
		"level=INFO",
		`msg="query completed"`,
		"service.name=energy-common",
		"request.id=req-1",
		"request.trace.id=trace-1",
		`request.sql="SELECT 1"`,
		"request.elapsed=12ms",
		"request.rows=1",
		`request.error="connection reset"`,
	} {
		assertContains(t, basic, want)
	}
}

func TestColorHandlerHonorsLevelAndAddsSource(t *testing.T) {
	var output bytes.Buffer
	handler := NewColorHandler(&output, WithLevel(slog.LevelInfo), WithAddSource(true))
	pcs := make([]uintptr, 1)
	runtime.Callers(1, pcs)
	record := slog.NewRecord(time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC), slog.LevelInfo, "started", pcs[0])
	record.AddAttrs(slog.String("component", "api"))

	if handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("Enabled(DEBUG) = true, want false")
	}
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	got := output.String()
	assertContains(t, got, ansiGreen+"source=")
	assertContains(t, got, "color_handler_test.go:")
	got = stripANSI(got)
	if !strings.Contains(got, "component=api") {
		t.Errorf("output %q does not contain record attribute", got)
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("output %q does not contain %q", got, want)
	}
}

func assertFieldColored(t *testing.T, output, key, value, color string) {
	t.Helper()
	if value == "" {
		assertContains(t, output, color+key+"=")
		return
	}
	assertContains(t, output, color+key+"="+value+ansiReset)
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}
