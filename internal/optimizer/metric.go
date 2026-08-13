package optimizer

// Metric is one scoring dimension of a placement. A lexicographic optimizer
// ranks candidates by multiple metrics in priority order (lexicographic): the
// primary metric decides, ties fall to the secondary, and so on. Lower rank
// values come first.
//
// Extension optimizers implement Metric (e.g. latency, carbon, budget) and
// either register it via RegisterMetric (so it can be used in strategies like
// "cost,latency") or build a strategy with NewStrategy.
type Metric interface {
	// Name identifies the metric (e.g. "cost", "time").
	Name() string
	// Rank returns how a candidate scores on this metric under the given
	// spot preference. Lower values rank earlier. Rank is called once per
	// candidate by the lexicographic optimizer; keep it cheap.
	Rank(c *Candidate, useSpot bool) float64
}

// metricBuilders maps metric names to their instances. Extensions register
// custom metrics via RegisterMetric; built-in "cost"/"time" are seeded here
// (their Metric definitions live in cost.go and time.go).
var metricBuilders = map[string]Metric{
	"cost": costMetric{},
	"time": timeMetric{},
}

// RegisterMetric adds a named metric so it can be used in strategies via
// ParseStrategy (e.g. ParseStrategy("cost,latency")). Overwrites any existing
// metric with the same name.
func RegisterMetric(name string, m Metric) {
	metricBuilders[name] = m
}

// MetricNames lists all registered metric names.
func MetricNames() []string {
	out := make([]string, 0, len(metricBuilders))
	for name := range metricBuilders {
		out = append(out, name)
	}
	return out
}
