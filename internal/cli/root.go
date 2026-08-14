// Package cli implements the gpi command-line interface.
//
// Output convention: everything a user runs a command to SEE is written via
// logging.CLIPrintf / logging.CLIPrintln — tables, confirm prompts, streamed
// (task)/(job)/(exec) progress lines, result summaries and error messages.
// These are interactive UX, NOT diagnostics: they always write to stdout and
// are never redirected by --log-file, so command output stays visible. The zap
// logger (logging.Get) is reserved for background/daemon diagnostics (server,
// provisioner, jobs scheduling, serve teardown failures, etc.).
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/acmestack/gpi/internal/backend"
	"github.com/acmestack/gpi/internal/buildinfo"
	"github.com/acmestack/gpi/internal/logging"
	"github.com/acmestack/gpi/internal/state"
)

// logger is the package logger, tagged with the module name. It is used for
// background/daemon diagnostics only (see the output convention above).
var logger = logging.WithName("cli")

// Execute runs the gpi CLI and exits with a non-zero status on error.
func Execute() {
	root := NewRootCommand()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// NewRootCommand builds the top-level gpi command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:     "gpi",
		Short:   "gpi: multi-cloud compute scheduling",
		Version: buildinfo.Version,
	}
	// `gpi --version` prints the ASCII-art banner followed by the version.
	root.SetVersionTemplate(fmt.Sprintf("%s\ngpi version {{.Version}}\n", Banner))

	// Persistent logging flags, layered over env vars and config file:
	// CLI flag > config file > env var > built-in default.
	var (
		logLevel  string
		logFile   string
		logFormat string
	)
	root.PersistentFlags().StringVar(&logLevel, "log-level", "", "log level: debug|info|warn|error (default: info)")
	root.PersistentFlags().StringVar(&logFile, "log-file", "", "write logs to this file instead of stdout")
	root.PersistentFlags().StringVar(&logFormat, "log-format", "", "log format: text|json (default: text)")
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		return setupLogging(logLevel, logFile, logFormat)
	}

	root.AddCommand(
		newLaunchCommand(),
		newStatusCommand(),
		newClusterCommand(),
		newDownCommand(),
		newStopCommand(),
		newStartCommand(),
		newExecCommand(),
		newOptimizeCommand(),
		newServeCommand(),
		newJobsCommand(),
		newServerCommand(),
	)
	return root
}

type ctx struct {
	store *state.Store
	prov  *backend.Manager
	dir   string
}

func newCtx() (*ctx, error) {
	dir, err := state.DefaultDir()
	if err != nil {
		return nil, err
	}
	store, err := state.Open()
	if err != nil {
		return nil, err
	}
	mgr, err := backend.New(store, dir)
	if err != nil {
		return nil, err
	}
	return &ctx{store: store, prov: mgr, dir: dir}, nil
}

func withCtx(fn func(*ctx, *cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		c, err := newCtx()
		if err != nil {
			return err
		}
		return fn(c, cmd, args)
	}
}

func cHome() string {
	if dir := os.Getenv("GPI_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gpi")
}
