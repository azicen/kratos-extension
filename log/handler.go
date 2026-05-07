package log

import (
	"context"
	"log/slog"

	klog "github.com/go-kratos/kratos/v2/log"
)

// 编译期断言：KratosHandler 实现了 slog.Handler 接口。
var _ slog.Handler = (*KratosHandler)(nil)

// Option 用于配置 KratosHandler。
type Option func(*KratosHandler)

// WithLevel 设置转发到 Kratos logger 的最低日志级别。
// 低于该级别的日志记录会在到达 Kratos 之前被静默丢弃。
// 未设置时默认为 slog.LevelInfo。
func WithLevel(l slog.Leveler) Option {
	return func(h *KratosHandler) {
		h.leveler = l
	}
}

// KratosHandler 是一个 slog.Handler，将日志输出委托给 Kratos log.Logger。
//
// 使用 NewKratosHandler 构造后传入 slog.New，即可将 log/slog 的调用桥接到 Kratos：
//
// 基本用法：使用默认的 Kratos 标准输出 logger
//	h := log.NewKratosHandler(klog.DefaultLogger)
//	slog.SetDefault(slog.New(h))
//	slog.Info("服务启动", "port", 8080)
//
// 自定义最低日志级别（允许 Debug 级别通过）
//	h := log.NewKratosHandler(kratosLogger, log.WithLevel(slog.LevelDebug))
//	logger := slog.New(h)
//	logger.Debug("调试信息", "trace_id", "abc123")
//
// 使用 WithAttrs 预设公共字段（每条日志都会携带）
//	logger := slog.New(log.NewKratosHandler(kratosLogger)).
//		With("service", "order-svc", "version", "v1.2.0")
//	logger.Info("请求处理完毕", "latency_ms", 42)
//
// 使用 WithGroup 为一组字段添加命名空间前缀
//	logger := slog.New(log.NewKratosHandler(kratosLogger)).
//		WithGroup("http")
//	logger.Info("收到请求", "method", "POST", "path", "/api/order")
// 输出键名：http.method, http.path
type KratosHandler struct {
	logger      klog.Logger
	leveler     slog.Leveler
	preKeyvals  []any  // 由 WithAttrs 预构建的键值对（已含 group 前缀）
	groupPrefix string // 由 WithGroup 累积的点分隔前缀，例如 "g1.g2."
}

// NewKratosHandler 创建一个以指定 Kratos logger 为后端的 KratosHandler。
func NewKratosHandler(logger klog.Logger, opts ...Option) *KratosHandler {
	h := &KratosHandler{
		logger:  logger,
		leveler: slog.LevelInfo,
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// Enabled 报告该 handler 是否处理指定级别的日志记录。
func (h *KratosHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.leveler.Level()
}

// Handle 将日志记录转发给 Kratos logger。
// 记录的消息以键 "msg" 输出。
// 所有 slog 属性会被展开为键值对，并应用 group 前缀。
func (h *KratosHandler) Handle(_ context.Context, r slog.Record) error {
	keyvals := make([]any, 0, 2+len(h.preKeyvals)+r.NumAttrs()*2)
	keyvals = append(keyvals, "msg", r.Message)
	keyvals = append(keyvals, h.preKeyvals...)
	r.Attrs(func(a slog.Attr) bool {
		appendAttr(&keyvals, h.groupPrefix, a)
		return true
	})
	return h.logger.Log(slogLevelToKratos(r.Level), keyvals...)
}

// WithAttrs 返回一个新的 Handler，在后续每条日志记录中都包含指定的属性。
// 属性的键会受当前已有 group 前缀的修饰。
func (h *KratosHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	newPreKeyvals := make([]any, 0, len(h.preKeyvals)+len(attrs)*2)
	newPreKeyvals = append(newPreKeyvals, h.preKeyvals...)
	for _, a := range attrs {
		appendAttr(&newPreKeyvals, h.groupPrefix, a)
	}
	return &KratosHandler{
		logger:      h.logger,
		leveler:     h.leveler,
		preKeyvals:  newPreKeyvals,
		groupPrefix: h.groupPrefix,
	}
}

// WithGroup 返回一个新的 Handler，将 name 追加到现有 group 序列。
// 后续所有属性的键都会以该 group 名称作为前缀修饰。
// 若 name 为空，则返回接收者本身，不做任何修改。
func (h *KratosHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &KratosHandler{
		logger:      h.logger,
		leveler:     h.leveler,
		preKeyvals:  h.preKeyvals,
		groupPrefix: h.groupPrefix + name + ".",
	}
}

// slogLevelToKratos 将 slog.Level 映射到最接近的 klog.Level。
func slogLevelToKratos(level slog.Level) klog.Level {
	switch {
	case level < slog.LevelInfo:
		return klog.LevelDebug
	case level < slog.LevelWarn:
		return klog.LevelInfo
	case level < slog.LevelError:
		return klog.LevelWarn
	default:
		return klog.LevelError
	}
}

// appendAttr 将属性 a 表示的键值对追加到 *keyvals，
// 并为每个键应用 prefix。Group 类型属性会使用点分隔递归展开。
func appendAttr(keyvals *[]any, prefix string, a slog.Attr) {
	// 在检查类型前先解析 LogValuer。
	a.Value = a.Value.Resolve()
	// 按 slog.Handler 规范跳过零值属性。
	if a.Equal(slog.Attr{}) {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		groupAttrs := a.Value.Group()
		if len(groupAttrs) == 0 {
			return
		}
		newPrefix := prefix
		if a.Key != "" {
			newPrefix = prefix + a.Key + "."
		}
		for _, ga := range groupAttrs {
			appendAttr(keyvals, newPrefix, ga)
		}
		return
	}
	*keyvals = append(*keyvals, prefix+a.Key, a.Value.Any())
}
