package logging

import (
	"gopkg.in/natefinch/lumberjack.v2"
)

// rotation returns a lumberjack writer enforcing the configured rotation
// policy, with sane defaults when fields are left at zero.
func rotation(cfg Config) *lumberjack.Logger {
	r := &lumberjack.Logger{Filename: cfg.File}
	if cfg.MaxSize > 0 {
		r.MaxSize = cfg.MaxSize
	} else {
		r.MaxSize = DefaultMaxSize
	}
	if cfg.MaxBackups > 0 {
		r.MaxBackups = cfg.MaxBackups
	} else {
		r.MaxBackups = DefaultMaxBackups
	}
	if cfg.MaxAge > 0 {
		r.MaxAge = cfg.MaxAge
	} else {
		r.MaxAge = DefaultMaxAge
	}
	// Compress defaults to true (gzip rotated files).
	r.Compress = cfg.Compress
	return r
}
