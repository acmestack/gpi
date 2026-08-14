package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want zapcore.Level
	}{
		{"", zapcore.InfoLevel},
		{"debug", zapcore.DebugLevel},
		{"DEBUG", zapcore.DebugLevel},
		{"warn", zapcore.WarnLevel},
		{"warning", zapcore.WarnLevel},
		{"error", zapcore.ErrorLevel},
		{"bogus", zapcore.InfoLevel},
	}
	for _, c := range cases {
		if got := parseLevel(c.in); got != c.want {
			t.Fatalf("parseLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestTextFormat(t *testing.T) {
	var buf bytes.Buffer
	l := build(newCore(&buf, zapcore.InfoLevel, true))
	l.Info("hello", zap.String("k", "v"))
	out := buf.String()
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected text format containing message, got %q", out)
	}
}

func TestSetupFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "app.log")
	Setup(Config{File: path})
	Get().Info("written", "to", "file")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "written") {
		t.Fatalf("log file missing message: %q", data)
	}
}

func TestSetupRotationDefaults(t *testing.T) {
	r := rotation(Config{File: "/tmp/x.log", Compress: true})
	if r.MaxSize != DefaultMaxSize || r.MaxBackups != DefaultMaxBackups ||
		r.MaxAge != DefaultMaxAge || !r.Compress {
		t.Fatalf("rotation defaults not applied: %+v", r)
	}
}

func TestWithNameAfterSetup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	Setup(Config{File: path})
	named := WithName("provisioner")
	named.Info("named after setup", "cluster", "c1")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "named after setup") {
		t.Fatalf("named log missing message: %q", out)
	}
	if !strings.Contains(out, "provisioner") {
		t.Fatalf("named log missing logger name: %q", out)
	}
}

func TestWithNameBeforeSetupStillFollowsSetup(t *testing.T) {
	named := WithName("optimizer")
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	Setup(Config{File: path})
	named.Info("named before setup")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "named before setup") {
		t.Fatalf("named log created before Setup must follow Setup file: %q", data)
	}
}

func TestLoggerWith(t *testing.T) {
	var buf bytes.Buffer
	base := &Logger{sugared: build(newCore(&buf, zapcore.InfoLevel, true)).Sugar()}
	base.With("svc", "meta").Info("msg")
	out := buf.String()
	if !strings.Contains(out, "svc") || !strings.Contains(out, "meta") {
		t.Fatalf("With fields missing from output: %q", out)
	}
}

func TestCLIPrintfWritesToStdout(t *testing.T) {
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	CLIPrintf("hello %s", "world")
	CLIPrintln("!")
	w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "hello world!") {
		t.Fatalf("CLIPrintf output = %q, want hello world!", out)
	}
}
