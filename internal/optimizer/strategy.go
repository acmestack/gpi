package optimizer

import (
	"fmt"
	"strings"
)

// This file provides strategy construction: NewStrategy builds an optimizer
// from Metric instances, ParseStrategy parses a priority-ordered metric-name
// list, and init registers the built-in single-metric optimizers. The actual
// lexicographic ranking implementation lives in lexicographic.go.

// NewStrategy builds an optimizer ranking candidates by the given metrics
// in priority order (first = primary, last = least important tie-break). With
// no metrics it falls back to a cost-only strategy.
func NewStrategy(metrics ...Metric) Optimizer {
	if len(metrics) == 0 {
		metrics = []Metric{costMetric{}}
	}
	return lexicographicOptimizer{metrics: metrics}
}

// ParseStrategy builds a lexicographic optimizer from a priority-ordered list of
// metric names, e.g. "cost,time" (cost primary, time tie-break). A single
// name yields a single-metric strategy equivalent to the named optimizer.
// Unknown metric names return an error listing the available ones.
func ParseStrategy(spec string) (Optimizer, error) {
	names := strings.Split(spec, ",")
	objs := make([]Metric, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		o, ok := metricBuilders[name]
		if !ok {
			return nil, fmt.Errorf("unknown metric %q (available: %s)", name, strings.Join(MetricNames(), ", "))
		}
		objs = append(objs, o)
	}
	if len(objs) == 0 {
		return nil, fmt.Errorf("empty optimizer strategy %q", spec)
	}
	return lexicographicOptimizer{metrics: objs}, nil
}

// registerBuiltins registers the single-metric optimizers "cost" and "time"
// so optimizer.Get("cost") / Get("time") resolve to ready-to-use strategies.
func init() {
	Register(NewStrategy(costMetric{}))
	Register(NewStrategy(timeMetric{}))
}
