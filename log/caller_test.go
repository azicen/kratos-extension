package log

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExtractPkgPath(t *testing.T) {
	tests := []struct {
		funcName string
		want     string
	}{
		{
			funcName: "192.168.1.189/rcsz/energy-edge/module/protocol/modbus_tcp/infras/poller.(*Poller).readTask",
			want:     "192.168.1.189/rcsz/energy-edge/module/protocol/modbus_tcp/infras/poller",
		},
		{
			funcName: "github.com/azicen/kratos-extension/log.TestExtractPkgPath",
			want:     "github.com/azicen/kratos-extension/log",
		},
		{
			funcName: "main.main",
			want:     "main",
		},
		{
			funcName: "net/http.(*Server).Serve",
			want:     "net/http",
		},
	}
	for _, tc := range tests {
		got := extractPkgPath(tc.funcName)
		if got != tc.want {
			t.Errorf("extractPkgPath(%q) = %q, want %q", tc.funcName, got, tc.want)
		}
	}
}

func TestCallerFromPC_ContainsGoFileAndLine(t *testing.T) {
	// 获取当前函数的 PC
	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	pc := pcs[0]

	result := callerFromPC(pc)
	if !strings.Contains(result, ".go:") {
		t.Errorf("callerFromPC() = %q, expected to contain '.go:'", result)
	}
	// 结果应包含当前测试文件名
	if !strings.Contains(result, "caller_test.go:") {
		t.Errorf("callerFromPC() = %q, expected to contain 'caller_test.go:'", result)
	}
}

func TestCallerFromPC_RelativePath(t *testing.T) {
	// 当 modulePath 已设置时，结果应为相对路径（不以 '/' 开头）
	if modulePath == "" {
		t.Skip("modulePath not available (not built with module support)")
	}

	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	pc := pcs[0]

	result := callerFromPC(pc)
	if strings.HasPrefix(result, "/") {
		t.Errorf("callerFromPC() = %q, expected relative path (not starting with '/')", result)
	}
	// 结果应以 "log/caller_test.go:" 开头（当前包在 module 内的路径）
	if !strings.HasPrefix(result, "log/caller_test.go:") {
		t.Errorf("callerFromPC() = %q, expected to start with 'log/caller_test.go:'", result)
	}
}

func TestHandle_CallerInjected(t *testing.T) {
	mock := &mockLogger{}
	h := NewKratosHandler(mock)

	// 构造一个带有效 PC 的 Record
	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "test", pcs[0])

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	val, ok := findVal(mock.keyvals, "caller")
	if !ok {
		t.Errorf("expected 'caller' in keyvals %v", mock.keyvals)
	}
	caller, ok := val.(string)
	if !ok {
		t.Errorf("expected caller to be string, got %T", val)
	}
	if !strings.Contains(caller, "caller_test.go:") {
		t.Errorf("caller = %q, expected to contain 'caller_test.go:'", caller)
	}
}

func TestHandle_CallerNotInjectedWhenPCZero(t *testing.T) {
	mock := &mockLogger{}
	h := NewKratosHandler(mock)

	// PC 为 0 的 Record
	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "test", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	if _, ok := findVal(mock.keyvals, "caller"); ok {
		t.Errorf("expected no 'caller' when PC is 0, keyvals=%v", mock.keyvals)
	}
}

func TestHandle_CallerBeforeAttrs(t *testing.T) {
	mock := &mockLogger{}
	h := NewKratosHandler(mock)

	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "test", pcs[0])
	r.AddAttrs(slog.String("key", "value"))

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}

	// keyvals 顺序应为: "msg", ..., "caller", ..., "key", ...
	callerIdx := -1
	keyIdx := -1
	for i := 0; i+1 < len(mock.keyvals); i += 2 {
		if k, ok := mock.keyvals[i].(string); ok {
			if k == "caller" {
				callerIdx = i
			}
			if k == "key" {
				keyIdx = i
			}
		}
	}
	if callerIdx < 0 {
		t.Fatal("caller not found in keyvals")
	}
	if keyIdx < 0 {
		t.Fatal("key not found in keyvals")
	}
	if callerIdx >= keyIdx {
		t.Errorf("caller (index %d) should appear before attrs (index %d)", callerIdx, keyIdx)
	}
}
