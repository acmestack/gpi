package provisioner

import (
	"bufio"
	"io"
	"strings"
)

type lineWriter struct {
	stream func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w.stream != nil {
		scanner := bufio.NewScanner(strings.NewReader(string(p)))
		for scanner.Scan() {
			w.stream(scanner.Text())
		}
	}
	return n, nil
}

var _ io.Writer = (*lineWriter)(nil)
