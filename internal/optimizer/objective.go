package optimizer

// Objective is one scoring dimension of a placement. A strategy optimizer
// ranks candidates by multiple objectives in priority order (lexicographic):
// the primary objective decides, ties fall to the secondary, and so on. Lower
// rank values come first.
//
// Extension optimizers implement Objective (e.g. latency, carbon, budget) and
// either register it via RegisterObjective (so it can be used in strategies
// like "cost,latency") or build a strategy with NewStrategy.
type Objective interface {
	// Name identifies the objective (e.g. "cost", "time").
	Name() string
	// Rank returns how a candidate scores on this objective under the given
	// spot preference. Lower values rank earlier. Rank is called once per
	// candidate by the strategy optimizer; keep it cheap.
	Rank(c *Candidate, useSpot bool) float64
}

// objectiveBuilders maps objective names to their instances. Extensions
// register custom objectives via RegisterObjective; built-in "cost"/"time" are
// seeded here (their Objective definitions live in cost.go and time.go).
var objectiveBuilders = map[string]Objective{
	"cost": costObjective{},
	"time": timeObjective{},
}

// RegisterObjective adds a named objective so it can be used in strategies via
// ParseStrategy (e.g. ParseStrategy("cost,latency")). Overwrites any existing
// objective with the same name.
func RegisterObjective(name string, o Objective) {
	objectiveBuilders[name] = o
}

// ObjectiveNames lists all registered objective names.
func ObjectiveNames() []string {
	out := make([]string, 0, len(objectiveBuilders))
	for name := range objectiveBuilders {
		out = append(out, name)
	}
	return out
}
