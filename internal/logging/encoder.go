package logging

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// build returns a zap logger over the given core. The caller annotation uses
// AddCallerSkip so the reported file:line is the real call site even though
// every call passes through the Logger wrapper and zap's SugaredLogger.
func build(core zapcore.Core) *zap.Logger {
	return zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
}

// newCore builds a zap core writing to w. text selects console encoding,
// otherwise JSON encoding is used.
func newCore(w interface{ Write([]byte) (int, error) }, level zapcore.Level, text bool) zapcore.Core {
	encCfg := zap.NewProductionEncoderConfig()
	if text {
		encCfg = zap.NewDevelopmentEncoderConfig()
	}
	var enc zapcore.Encoder
	if text {
		enc = zapcore.NewConsoleEncoder(encCfg)
	} else {
		enc = zapcore.NewJSONEncoder(encCfg)
	}
	return zapcore.NewCore(enc, zapcore.AddSync(w), level)
}
