package logging

import (
	"fmt"
	"os"
)

// CLIPrint writes CLI output to stdout without a trailing newline.
func CLIPrint(a ...any) {
	fmt.Fprint(os.Stdout, a...)
}

// CLIPrintf writes formatted CLI output to stdout. It is never redirected by
// log configuration, so command results and prompts stay visible to the user
// even when diagnostics go to a file.
func CLIPrintf(format string, a ...any) {
	fmt.Fprintf(os.Stdout, format, a...)
}

// CLIPrintln writes CLI output to stdout with a trailing newline.
func CLIPrintln(a ...any) {
	fmt.Fprintln(os.Stdout, a...)
}
