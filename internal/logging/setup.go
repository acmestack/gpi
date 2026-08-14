package logging

import (
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	mu      sync.RWMutex
	current *Logger
)

func init() {
	current = &Logger{sugared: build(newCore(os.Stdout, zapcore.InfoLevel, true)).Sugar()}
}

// Get returns the current default logger. It is safe for concurrent use.
func Get() *Logger {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Setup configures the package default logger. An empty File keeps stdout.
// File output uses lumberjack for rotation, gzip compression and backup
// retention; rotation fields are applied with sane defaults when unset.
func Setup(cfg Config) {
	level := parseLevel(cfg.Level)
	if cfg.File == "" {
		core := newCore(os.Stdout, level, cfg.Format != "json")
		set(build(core))
		return
	}
	rot := rotation(cfg)
	core := newCore(rot, level, cfg.Format != "json")
	set(build(core))
}

func set(l *zap.Logger) {
	mu.Lock()
	defer mu.Unlock()
	current = &Logger{sugared: l.Sugar()}
	_ = zap.ReplaceGlobals(l)
}
