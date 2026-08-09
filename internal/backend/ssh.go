package backend

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"

	"github.com/acmestack/gpi/internal/state"
)

// sshRun executes a script on a host via the system ssh binary. Returns the
// captured combined output and the exit code.
func sshRun(ctx context.Context, target *state.SSHTarget, script string, stream func(string)) (string, int, error) {
	args := []string{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null"}
	if target.Port != 0 {
		args = append(args, "-p", strconv.Itoa(target.Port))
	}
	if target.Key != "" {
		args = append(args, "-i", target.Key)
	}
	user := target.User
	if user == "" {
		user = "root"
	}
	args = append(args, user+"@"+target.Host, "bash -s")

	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = strings.NewReader(script)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			return strings.TrimSpace(buf.String()), -1, err
		}
	}
	out := strings.TrimSpace(buf.String())
	if stream != nil && out != "" {
		for _, line := range strings.Split(out, "\n") {
			stream(line)
		}
	}
	return out, code, nil
}

// sshWait waits until a host accepts SSH connections.
func sshWait(ctx context.Context, target *state.SSHTarget) error {
	_, _, err := sshRun(ctx, target, "true", nil)
	return err
}
