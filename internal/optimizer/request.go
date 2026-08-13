package optimizer

import (
	"github.com/acmestack/gpi/internal/task"
)

// Options controls the placement search space.
type Options struct {
	NumNodes      int
	UseSpot       bool
	Cloud         string
	Region        string
	Zone          string
	MaxCandidates int
}

// defaultOptions returns Options with NumNodes=1 and MaxCandidates=10.
func defaultOptions() *Options {
	return &Options{NumNodes: 1, MaxCandidates: 10}
}

// Request is everything an optimizer needs to rank placement candidates.
// Cloud metadata (specs/prices) is always read from the shared defaultMeta.
type Request struct {
	Resources *task.Resources
	Options   *Options
}
