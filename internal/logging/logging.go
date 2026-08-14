// Package logging provides two output channels.
//
// The diagnostic channel is a structured logger built on uber-go/zap (the
// de-facto standard Go logging library), paired with lumberjack for file
// output so rotation is size-based with gzip compression and backup
// retention. The default logger writes to stdout at Info level; Setup can
// redirect output to a rotating file, raise/lower the level, or switch to
// JSON encoding. Packages obtain the current logger via Get, so they never
// need to thread one through. Logger is a thin wrapper around zap's
// SugaredLogger: level methods take the message followed by key/value pairs,
// so call sites stay concise while customization (global fields, sampling,
// hooks) stays centralized in this package.
//
// The CLI channel (CLIPrintf/CLIPrintln) writes user-facing command output to
// stdout. It is deliberately separate from the diagnostic channel:
// --log-file / logging configuration never redirects it, so command results
// remain visible.
//
// Files in this package:
//   - logging.go: package doc and the Logger wrapper (level methods).
//   - setup.go: the default logger lifecycle (Get/Setup).
//   - config.go: Config and the rotation defaults.
//   - encoder.go: zap core/encoder construction (text vs json).
//   - rotate.go: lumberjack file rotation policy.
//   - level.go: level string parsing.
//   - cli.go: the CLIPrintf/CLIPrintln user-facing output channel.
package logging

import "go.uber.org/zap"

// Logger is a thin wrapper around zap's SugaredLogger. Level methods take a
// message followed by alternating key/value pairs, e.g.
//
//	Log.Info("launching", "cluster", name, "nodes", n)
//
// so customization (global fields, sampling, hooks) stays centralized in this
// package instead of every call site. A Logger tagged via WithName resolves
// the base logger through Get on every call, so Setup (level/file/format)
// applied after the package-level variable was created still takes effect.
type Logger struct {
	sugared *zap.SugaredLogger
	name    string
}

// base returns the underlying SugaredLogger. Named loggers resolve the current
// default through Get each call (see Named); plain loggers use their cached
// sugared instance.
func (l *Logger) base() *zap.SugaredLogger {
	if l.name != "" {
		return Get().sugared.Named(l.name)
	}
	return l.sugared
}

// Debug logs at Debug level.
func (l *Logger) Debug(msg string, kvs ...any) { l.base().Debugw(msg, kvs...) }

// Info logs at Info level.
func (l *Logger) Info(msg string, kvs ...any) { l.base().Infow(msg, kvs...) }

// Warn logs at Warn level.
func (l *Logger) Warn(msg string, kvs ...any) { l.base().Warnw(msg, kvs...) }

// Error logs at Error level.
func (l *Logger) Error(msg string, kvs ...any) { l.base().Errorw(msg, kvs...) }

// With returns a Logger that always attaches the given key/value pairs.
func (l *Logger) With(kvs ...any) *Logger { return &Logger{sugared: l.base().With(kvs...)} }

// WithName returns a Logger tagged with a module name, e.g. "provisioner".
// The name is derived from the current default logger, so Setup configuration
// (level/file/format) still applies. Packages typically assign it to a
// package-level variable once.
func WithName(name string) *Logger { return &Logger{name: name} }
