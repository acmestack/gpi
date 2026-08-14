package logging

// Default rotation policy used when file output is enabled but the caller
// does not set the rotation fields.
const (
	DefaultMaxSize    = 100 // MB before a new file is started
	DefaultMaxBackups = 5   // rotated files kept on disk
	DefaultMaxAge     = 30  // days before a rotated file is removed
)

// Config controls the logger created by Setup.
type Config struct {
	// Level is the minimum severity: debug|info|warn|error (default info).
	Level string
	// File is the log output path; empty means stdout.
	File string
	// Format is "text" (default) or "json".
	Format string
	// Rotation fields only apply when File is set.
	// MaxSize is the file size in MB that triggers rotation (default 100).
	MaxSize int
	// MaxBackups is the number of rotated files to keep (default 5).
	MaxBackups int
	// MaxAge is the number of days to keep rotated files (default 30).
	MaxAge int
	// Compress enables gzip compression of rotated files (default true).
	Compress bool
}
