package optimizer

// This file holds the named optimizer registry. The Optimizer interface and
// the resolution entry points (Get/Resolve) live in optimizer.go; this file
// only manages the name -> Optimizer map.

var registry = map[string]Optimizer{}

// DefaultName is the name of the built-in cost-based optimizer.
const DefaultName = "cost"

// Register adds an optimizer implementation by name.
func Register(o Optimizer) {
	if o.Name() == "" {
		panic("optimizer: registered optimizer must have a name")
	}
	registry[o.Name()] = o
}

// Names lists all registered optimizer names.
func Names() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	return out
}

// Default returns the default optimizer ("cost").
func Default() Optimizer {
	return registry[DefaultName]
}
