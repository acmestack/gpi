package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/acmestack/gpi/internal/gpilet"
	"github.com/acmestack/gpi/internal/logging"
)

const usage = `gpilet - gpi node agent (skylet equivalent)

Usage:
  gpilet serve [--dir DIR] [--interval SECS]   run as a daemon on the node,
                                               writing status.json into DIR
  gpilet status [--dir DIR]                    print the latest node status (JSON)
`

// logger is the package logger, tagged with the module name.
var logger = logging.WithName("gpilet")

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		serveCmd(os.Args[2:])
	case "status":
		statusCmd(os.Args[2:])
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func parseFlags(args []string) (dir string, interval time.Duration) {
	fs := flag.NewFlagSet("gpilet", flag.ExitOnError)
	dir = "/var/lib/gpilet"
	intervalSecs := 10
	fs.StringVar(&dir, "dir", dir, "directory to write status.json into")
	fs.IntVar(&intervalSecs, "interval", intervalSecs, "status collection interval (seconds)")
	_ = fs.Parse(args)
	return dir, time.Duration(intervalSecs) * time.Second
}

func serveCmd(args []string) {
	dir, interval := parseFlags(args)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "gpilet:", err)
		os.Exit(1)
	}
	start := time.Now()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var prev *gpilet.Status
	write := func() {
		st, err := gpilet.Collect(prev)
		if err != nil {
			logger.Error("collect failed", "error", err)
			return
		}
		st.GpiletUptime = int64(time.Since(start).Seconds())
		prev = st
		data, err := json.MarshalIndent(st, "", "  ")
		if err != nil {
			return
		}
		tmp := filepath.Join(dir, "status.json.tmp")
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			logger.Error("write status failed", "error", err)
			return
		}
		if err := os.Rename(tmp, filepath.Join(dir, "status.json")); err != nil {
			logger.Error("rename status failed", "error", err)
			return
		}
	}

	write()
	logger.Info("gpilet serving", "dir", dir, "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			write()
		case <-sigCh:
			write()
			logger.Info("gpilet stopped")
			return
		}
	}
}

func statusCmd(args []string) {
	dir, _ := parseFlags(args)
	data, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "gpilet: no status yet at %s/status.json: %v\n", dir, err)
		os.Exit(1)
	}
	logging.CLIPrint(string(data))
}
