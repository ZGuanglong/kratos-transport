// Package log 提供 kratos v2 风格日志 API 的 slog 实现。
//
// kratos v3 的 log 包只负责构建 slog.Handler/Logger，不再提供全局 logger、
// log.Helper、包级 Infof/Errorf 等便捷函数。kratos-transport 内部大量使用这些
// 便捷函数，这里用标准库 log/slog 补齐，保持调用方代码不变。
package log

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Level 与 slog.Level 对齐，保留 v2 风格命名。
type Level = slog.Level

const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
	LevelFatal = slog.Level(12)
)

// Logger 与 kratos v2 的 log.Logger 接口保持一致。
type Logger interface {
	Log(level Level, keyvals ...any) error
}

// Helper 与 kratos v2 的 log.Helper 保持一致的常用方法。
type Helper struct {
	logger Logger
	msgKey string
	kv     []any
}

// NewHelper 创建 Helper。
func NewHelper(logger Logger, opts ...HelperOption) *Helper {
	h := &Helper{logger: logger, msgKey: "msg"}
	for _, o := range opts {
		o(h)
	}
	return h
}

// HelperOption 配置 Helper。
type HelperOption func(*Helper)

// WithMessageKey 指定消息字段名。
func WithMessageKey(k string) HelperOption {
	return func(h *Helper) { h.msgKey = k }
}

// Helper 创建带额外属性的子 Helper。
func (h *Helper) With(kv ...any) *Helper {
	merged := make([]any, 0, len(h.kv)+len(kv))
	merged = append(merged, h.kv...)
	merged = append(merged, kv...)
	return &Helper{logger: h.logger, msgKey: h.msgKey, kv: merged}
}

// WithContext 兼容 v2 调用，ctx 目前仅用于透传。
func (h *Helper) WithContext(_ context.Context) *Helper { return h }

func (h *Helper) log(level Level, args ...any) {
	kv := make([]any, 0, len(h.kv)+2)
	kv = append(kv, h.kv...)
	kv = append(kv, h.msgKey, fmt.Sprint(args...))
	_ = h.logger.Log(level, kv...)
}

func (h *Helper) logf(level Level, format string, args ...any) {
	kv := make([]any, 0, len(h.kv)+2)
	kv = append(kv, h.kv...)
	kv = append(kv, h.msgKey, fmt.Sprintf(format, args...))
	_ = h.logger.Log(level, kv...)
}

// Debug logs a message at debug level.
func (h *Helper) Debug(a ...any) { h.log(LevelDebug, a...) }

// Debugf logs a formatted message at debug level.
func (h *Helper) Debugf(f string, a ...any) { h.logf(LevelDebug, f, a...) }

// Info logs a message at info level.
func (h *Helper) Info(a ...any) { h.log(LevelInfo, a...) }

// Infof logs a formatted message at info level.
func (h *Helper) Infof(f string, a ...any) { h.logf(LevelInfo, f, a...) }

// Warn logs a message at warn level.
func (h *Helper) Warn(a ...any) { h.log(LevelWarn, a...) }

// Warnf logs a formatted message at warn level.
func (h *Helper) Warnf(f string, a ...any) { h.logf(LevelWarn, f, a...) }

// Error logs a message at error level.
func (h *Helper) Error(a ...any) { h.log(LevelError, a...) }

// Errorf logs a formatted message at error level.
func (h *Helper) Errorf(f string, a ...any) { h.logf(LevelError, f, a...) }

// Fatal logs a message at fatal level（不退出进程，保持库行为可控）。
func (h *Helper) Fatal(a ...any) { h.log(LevelFatal, a...) }

// Fatalf logs a formatted message at fatal level.
func (h *Helper) Fatalf(f string, a ...any) { h.logf(LevelFatal, f, a...) }

// Logger 返回底层 Logger。
func (h *Helper) Logger() Logger { return h.logger }

// slogAdapter 把 slog.Logger 适配成 Logger 接口。
type slogAdapter struct {
	l *slog.Logger
}

// NewSlogAdapter 把 slog.Logger 包装成 Logger。
func NewSlogAdapter(l *slog.Logger) Logger { return &slogAdapter{l: l} }

func (a *slogAdapter) Log(level Level, keyvals ...any) error {
	if a == nil || a.l == nil {
		return nil
	}
	attrs := make([]any, 0, len(keyvals))
	msgs := make([]string, 0, len(keyvals)/2+1)
	for i := 0; i+1 < len(keyvals); i += 2 {
		if k, ok := keyvals[i].(string); ok {
			attrs = append(attrs, k, keyvals[i+1])
			msgs = append(msgs, fmt.Sprint(keyvals[i+1]))
			continue
		}
		msgs = append(msgs, fmt.Sprint(keyvals[i], keyvals[i+1]))
	}
	if len(keyvals)%2 == 1 {
		msgs = append(msgs, fmt.Sprint(keyvals[len(keyvals)-1]))
	}
	a.l.Log(context.Background(), slog.Level(level), strings.Join(msgs, " "), attrs...)
	return nil
}

var (
	mu     sync.RWMutex
	global Logger
)

// SetLogger 设置全局 logger（传入 slog.Logger 或任何实现 Logger 的对象）。
func SetLogger(l any) {
	mu.Lock()
	defer mu.Unlock()
	switch v := l.(type) {
	case nil:
		global = nil
	case *slog.Logger:
		global = NewSlogAdapter(v)
	case Logger:
		global = v
	}
}

// GetLogger 返回全局 logger，未设置时返回 stderr 文本输出的默认 logger。
func GetLogger() Logger {
	mu.RLock()
	l := global
	mu.RUnlock()
	if l != nil {
		return l
	}
	return NewSlogAdapter(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: LevelInfo})))
}

// Debug logs a message at debug level.
func Debug(a ...any) { _ = GetLogger().Log(LevelDebug, a...) }

// Log 包级日志入口，兼容 v2 的 log.Log(level, keyvals...)；
// 允许把 keyvals 以 []any 形式整体传入（rocketmq 等子模块的用法）。
func Log(level Level, keyvals ...any) {
	flat := make([]any, 0, len(keyvals))
	for _, kv := range keyvals {
		if s, ok := kv.([]any); ok {
			flat = append(flat, s...)
			continue
		}
		flat = append(flat, kv)
	}
	_ = GetLogger().Log(level, flat...)
}

// Debugf logs a formatted message at debug level.
func Debugf(f string, a ...any) { _ = GetLogger().Log(LevelDebug, fmt.Sprintf(f, a...)) }

// Info logs a message at info level.
func Info(a ...any) { _ = GetLogger().Log(LevelInfo, a...) }

// Infof logs a formatted message at info level.
func Infof(f string, a ...any) { _ = GetLogger().Log(LevelInfo, fmt.Sprintf(f, a...)) }

// Warn logs a message at warn level.
func Warn(a ...any) { _ = GetLogger().Log(LevelWarn, a...) }

// Warnf logs a formatted message at warn level.
func Warnf(f string, a ...any) { _ = GetLogger().Log(LevelWarn, fmt.Sprintf(f, a...)) }

// Error logs a message at error level.
func Error(a ...any) { _ = GetLogger().Log(LevelError, a...) }

// Errorf logs a formatted message at error level.
func Errorf(f string, a ...any) { _ = GetLogger().Log(LevelError, fmt.Sprintf(f, a...)) }

// Fatal logs a message at fatal level.
func Fatal(a ...any) { _ = GetLogger().Log(LevelFatal, a...) }

// Fatalf logs a formatted message at fatal level.
func Fatalf(f string, a ...any) { _ = GetLogger().Log(LevelFatal, fmt.Sprintf(f, a...)) }
