package log

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewKratosHandler_UsesNativeSlogHandler(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewKratosHandler(WithWriter(&output), WithLevel(slog.LevelDebug)))

	logger.WithGroup("request").Info("handled", "method", "GET")

	got := output.String()
	if !strings.Contains(got, "msg=handled") {
		t.Errorf("output = %q, want message", got)
	}
	if !strings.Contains(got, "request.method=GET") {
		t.Errorf("output = %q, want grouped attribute", got)
	}
}

func TestNewKratosHandler_RespectsLevel(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewKratosHandler(WithWriter(&output), WithLevel(slog.LevelWarn)))

	logger.Info("ignored")
	logger.Warn("emitted")

	got := output.String()
	if strings.Contains(got, "ignored") {
		t.Errorf("output = %q, should not contain info message", got)
	}
	if !strings.Contains(got, "msg=emitted") {
		t.Errorf("output = %q, want warning message", got)
	}
}
