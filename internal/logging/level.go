package logging

import (
	"strings"

	"go.uber.org/zap/zapcore"
)

// parseLevel maps a level string to a zapcore.Level. Unknown/empty values
// fall back to Info.
func parseLevel(s string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}
