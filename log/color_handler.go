// Package logging provides application logging implementations.
package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

const (
	ansiReset  = "\x1b[0m"
	ansiGray   = "\x1b[90m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiPurple = "\x1b[35m"
	ansiCyan   = "\x1b[36m"
	ansiBlue   = "\x1b[34m"
)

var _ slog.Handler = (*ColorHandler)(nil)

// ColorHandler writes slog records with ANSI colors for levels and common fields.
type ColorHandler struct {
	writer    io.Writer
	level     slog.Leveler
	addSource bool
	attrs     []groupedAttr
	groups    []string
	mu        *sync.Mutex
}

type groupedAttr struct {
	attr   slog.Attr
	groups []string
}

// HandlerOption configures a ColorHandler.
type HandlerOption func(*ColorHandler)

// WithLevel sets the minimum log level.
func WithLevel(level slog.Leveler) HandlerOption {
	return func(handler *ColorHandler) {
		handler.level = level
	}
}

// WithAddSource toggles inclusion of the source file and line.
func WithAddSource(addSource bool) HandlerOption {
	return func(handler *ColorHandler) {
		handler.addSource = addSource
	}
}

// NewColorHandler creates an ANSI color slog handler.
func NewColorHandler(writer io.Writer, options ...HandlerOption) *ColorHandler {
	handler := &ColorHandler{
		writer: writer,
		level:  slog.LevelInfo,
		mu:     new(sync.Mutex),
	}
	for _, option := range options {
		option(handler)
	}
	return handler
}

// Enabled reports whether records at level should be logged.
func (h *ColorHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Handle writes one complete log record atomically.
func (h *ColorHandler) Handle(_ context.Context, record slog.Record) error {
	var builder strings.Builder
	writeField(&builder, "time", record.Time.Format("2006-01-02T15:04:05.000Z07:00"), record.Level)
	builder.WriteByte(' ')
	writeColoredField(&builder, "level", record.Level.String(), levelColor(record.Level))
	builder.WriteByte(' ')
	writeColoredField(&builder, "msg", record.Message, levelColor(record.Level))

	if h.addSource && record.PC != 0 {
		frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()
		if frame.File != "" {
			builder.WriteByte(' ')
			writeField(&builder, "source", frame.File+":"+strconv.Itoa(frame.Line), record.Level)
		}
	}

	for _, grouped := range h.attrs {
		h.writeAttr(&builder, grouped.attr, record.Level, grouped.groups)
	}
	record.Attrs(func(attr slog.Attr) bool {
		h.writeAttr(&builder, attr, record.Level, h.groups)
		return true
	})
	builder.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.writer, builder.String())
	return err
}

// WithAttrs returns a handler that includes attrs in every record.
func (h *ColorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append([]groupedAttr{}, h.attrs...)
	for _, attr := range attrs {
		clone.attrs = append(clone.attrs, groupedAttr{
			attr:   attr,
			groups: append([]string{}, h.groups...),
		})
	}
	return &clone
}

// WithGroup returns a handler that qualifies following attrs with name.
func (h *ColorHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.groups = append(append([]string{}, h.groups...), name)
	return &clone
}

func (h *ColorHandler) writeAttr(builder *strings.Builder, attr slog.Attr, level slog.Level, groups []string) {
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() == slog.KindGroup {
		childGroups := append(append([]string{}, groups...), attr.Key)
		for _, child := range attr.Value.Group() {
			h.writeAttr(builder, child, level, childGroups)
		}
		return
	}
	if attr.Key == "" {
		return
	}
	key := strings.Join(append(append([]string{}, groups...), attr.Key), ".")
	builder.WriteByte(' ')
	writeField(builder, key, attr.Value.String(), level)
}

func writeField(builder *strings.Builder, key, value string, level slog.Level) {
	fieldKey := key[strings.LastIndex(key, ".")+1:]
	switch {
	case key == "time":
		writeColoredField(builder, key, value, ansiGray)
	case key == "source":
		writeColoredField(builder, key, value, levelColor(level))
	case key == "service.id" || key == "service.name" || key == "service.version" || strings.HasSuffix(key, ".service.id") || strings.HasSuffix(key, ".service.name") || strings.HasSuffix(key, ".service.version"):
		writeColored(builder, ansiGray, key+"="+value)
	case fieldKey == "id":
		if key == "trace.id" || key == "span.id" || strings.HasSuffix(key, ".trace.id") || strings.HasSuffix(key, ".span.id") {
			writeColored(builder, ansiPurple, key+"="+value)
			return
		}
		builder.WriteString(fmt.Sprintf("%s=%s", key, value))
	case fieldKey == "trace_id" || fieldKey == "span_id":
		writeColoredField(builder, key, value, ansiPurple)
	case fieldKey == "err" || fieldKey == "error":
		writeColored(builder, ansiRed, key+"="+value)
	case fieldKey == "sql":
		writeColoredField(builder, key, value, ansiCyan)
	case fieldKey == "elapsed":
		writeColoredField(builder, key, value, ansiGreen)
	case fieldKey == "rows":
		writeColoredField(builder, key, value, ansiBlue)
	default:
		builder.WriteString(fmt.Sprintf("%s=%s", key, value))
	}
}

func writeColoredField(builder *strings.Builder, key, value, color string) {
	writeColored(builder, color, key+"="+value)
}

func writeColored(builder *strings.Builder, color, value string) {
	builder.WriteString(color)
	builder.WriteString(value)
	builder.WriteString(ansiReset)
}

func levelColor(level slog.Level) string {
	switch {
	case level <= slog.LevelDebug:
		return ansiGray
	case level < slog.LevelWarn:
		return ansiGreen
	case level < slog.LevelError:
		return ansiYellow
	default:
		return ansiRed
	}
}
