package log

import (
	"io"
	"log/slog"

	klog "github.com/go-kratos/kratos/v3/log"
)

// Option 是 Kratos v3 原生日志 Handler 的配置选项。
type Option = klog.Option

// WithLevel 设置日志 Handler 的最低日志级别。
func WithLevel(level slog.Leveler) Option {
	return klog.WithLevel(level)
}

// WithWriter 设置日志输出目标。
func WithWriter(writer io.Writer) Option {
	return klog.WithWriter(writer)
}

// WithAddSource 设置是否输出调用源位置。
func WithAddSource(enabled bool) Option {
	return klog.WithAddSource(enabled)
}

// NewKratosHandler 创建 Kratos v3 原生 slog Handler。
//
// Kratos v3 已基于 slog 实现日志能力，因此不再需要将旧版 klog.Logger
// 转发为 slog.Handler 的桥接器。调用方可使用 slog.New 返回的 Handler，或
// 通过 klog.SetDefault(klog.NewLogger(NewKratosHandler(...))) 同步 Kratos 与 slog 默认日志器。
func NewKratosHandler(opts ...Option) slog.Handler {
	return klog.NewHandler(opts...)
}
