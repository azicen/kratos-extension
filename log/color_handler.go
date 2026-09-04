// Package logging provides application logging implementations.
package log

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
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

// ColorHandler wraps a standard library slog.TextHandler and post-processes
// its output to inject ANSI colors for levels and common fields.
type ColorHandler struct {
	slog.Handler
}

// HandlerOption configures the underlying slog.HandlerOptions.
type HandlerOption func(*slog.HandlerOptions)

// WithLevel sets the minimum log level.
func WithLevel(level slog.Leveler) HandlerOption {
	return func(opts *slog.HandlerOptions) {
		opts.Level = level
	}
}

// WithAddSource toggles inclusion of the source file and line.
func WithAddSource(addSource bool) HandlerOption {
	return func(opts *slog.HandlerOptions) {
		opts.AddSource = addSource
	}
}

// NewColorHandler creates an ANSI color slog handler backed by slog.TextHandler.
func NewColorHandler(writer io.Writer, options ...HandlerOption) *ColorHandler {
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 && attr.Key == slog.TimeKey {
				attr.Value = slog.StringValue(attr.Value.Time().Format("2006-01-02T15:04:05.000Z07:00"))
			}
			return attr
		},
	}
	for _, option := range options {
		option(opts)
	}
	return &ColorHandler{Handler: slog.NewTextHandler(&colorWriter{writer: writer}, opts)}
}

// WithAttrs returns a handler that includes attrs in every record.
func (h *ColorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ColorHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup returns a handler that qualifies following attrs with name.
func (h *ColorHandler) WithGroup(name string) slog.Handler {
	return &ColorHandler{Handler: h.Handler.WithGroup(name)}
}

// colorWriter post-processes slog.TextHandler's key=value output to inject
// ANSI colors, without reimplementing the text formatting itself.
type colorWriter struct {
	writer io.Writer
}

// Write scans the already-formatted "key=value key=value ..." line emitted by
// slog.TextHandler and wraps colorable fields with ANSI escapes. It avoids
// regexp in favor of a single-pass byte scan to keep the hot logging path fast.
func (w *colorWriter) Write(p []byte) (int, error) {
	n := len(p)
	hasNewline := len(p) > 0 && p[len(p)-1] == '\n'
	line := p
	if hasNewline {
		line = p[:len(p)-1]
	}

	level := ansiGreen
	buf := make([]byte, 0, len(line)+64)
	first := true
	for start := 0; start < len(line); {
		end := start
		inQuotes := false
		for end < len(line) {
			c := line[end]
			if c == '"' {
				inQuotes = !inQuotes
			} else if c == ' ' && !inQuotes {
				break
			}
			end++
		}
		token := line[start:end]

		if !first {
			buf = append(buf, ' ')
		}
		first = false

		if eq := bytes.IndexByte(token, '='); eq >= 0 {
			key := token[:eq]
			value := token[eq+1:]
			if string(key) == "level" {
				level = levelColor(string(value))
			}
			if color, ok := fieldColor(string(key), level); ok {
				buf = append(buf, color...)
				buf = append(buf, token...)
				buf = append(buf, ansiReset...)
			} else {
				buf = append(buf, token...)
			}
		} else {
			buf = append(buf, token...)
		}

		start = end + 1
	}
	if hasNewline {
		buf = append(buf, '\n')
	}

	if _, err := w.writer.Write(buf); err != nil {
		return 0, err
	}
	return n, nil
}

// fieldColor returns the ANSI color for a key=value field, if any.
func fieldColor(key, levelColor string) (string, bool) {
	fieldKey := key[strings.LastIndex(key, ".")+1:]
	switch {
	case key == "time":
		return ansiGray, true
	case key == "level" || key == "msg" || key == "source":
		return levelColor, true
	case key == "service.id" || key == "service.name" || key == "service.version" ||
		strings.HasSuffix(key, ".service.id") || strings.HasSuffix(key, ".service.name") || strings.HasSuffix(key, ".service.version"):
		return ansiGray, true
	case fieldKey == "id":
		if key == "trace.id" || key == "span.id" || strings.HasSuffix(key, ".trace.id") || strings.HasSuffix(key, ".span.id") {
			return ansiPurple, true
		}
		return "", false
	case fieldKey == "trace_id" || fieldKey == "span_id":
		return ansiPurple, true
	case fieldKey == "err" || fieldKey == "error":
		return ansiRed, true
	case fieldKey == "sql":
		return ansiCyan, true
	case fieldKey == "elapsed":
		return ansiGreen, true
	case fieldKey == "rows":
		return ansiBlue, true
	default:
		return "", false
	}
}

func levelColor(level string) string {
	switch {
	case strings.HasPrefix(level, "DEBUG"):
		return ansiGray
	case strings.HasPrefix(level, "WARN"):
		return ansiYellow
	case strings.HasPrefix(level, "ERROR"):
		return ansiRed
	default:
		return ansiGreen
	}
}
