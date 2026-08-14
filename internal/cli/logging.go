package cli

import (
	"os"

	"github.com/acmestack/gpi/internal/config"
	"github.com/acmestack/gpi/internal/logging"
)

// setupLogging initializes the package logger from (in precedence order):
// CLI flag, config file (logging: section), env var, built-in default. The
// default output is stdout at Info level in text format.
func setupLogging(level, file, format string) error {
	cfg := config.Load()
	lvl := firstNonEmpty(level, os.Getenv("GPI_LOG_LEVEL"), cfg.LogLevel())
	filePath := firstNonEmpty(file, os.Getenv("GPI_LOG_FILE"), cfg.LogFile())
	fmtStr := firstNonEmpty(format, os.Getenv("GPI_LOG_FORMAT"), cfg.LogFormat())

	compress := true
	if c := cfg.LogCompress(); c != nil {
		compress = *c
	}
	logging.Setup(logging.Config{
		Level:      lvl,
		File:       filePath,
		Format:     fmtStr,
		MaxSize:    cfg.LogMaxSize(),
		MaxBackups: cfg.LogMaxBackups(),
		MaxAge:     cfg.LogMaxAge(),
		Compress:   compress,
	})
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
