package optimizer

// Launch is a single placement decision: which cloud/region/instance type to
// use, at what cost. It is one candidate row of a Plan.
type Launch struct {
	Cloud        string
	Region       string
	Zone         string
	InstanceType string
	NumNodes     int
	Accelerators map[string]int
	VCPUs        int
	MemoryGiB    float64
	OnDemandCost float64
	SpotCost     float64
	UseSpot      bool
	// EstimatedTime is the estimated runtime in hours for this placement. It
	// is only populated by optimizers that minimize run time (e.g. "time").
	EstimatedTime float64
	Order         int
}

// CostPerHour returns the hourly cost of this launch given its spot choice.
// When the chosen mode's price is missing, the other mode's price is used as
// an estimate so ranking and display stay meaningful even with partial data.
func (l *Launch) CostPerHour() float64 {
	if l.UseSpot {
		if l.SpotCost > 0 {
			return l.SpotCost
		}
		if l.OnDemandCost > 0 {
			return l.OnDemandCost * 0.3
		}
		return 0
	}
	if l.OnDemandCost > 0 {
		return l.OnDemandCost
	}
	if l.SpotCost > 0 {
		return l.SpotCost * 3
	}
	return 0
}

// TotalCostPerHour returns the total hourly cost across all nodes.
func (l *Launch) TotalCostPerHour() float64 {
	return l.CostPerHour() * float64(l.NumNodes)
}

// Plan is the optimizer output: ranked placement candidates plus total cost.
type Plan struct {
	Launches []*Launch
	// TotalCostPerHour is the hourly cost of the top candidate.
	TotalCostPerHour float64
	// TotalEstimatedTime is the estimated total runtime in hours of the top
	// candidate; only meaningful for time-minimizing optimizers.
	TotalEstimatedTime float64
}

// LaunchTypes returns the instance types of the plan's launches in ranked
// order (useful for tests and summaries).
func (p *Plan) LaunchTypes() []string {
	out := make([]string, 0, len(p.Launches))
	for _, l := range p.Launches {
		out = append(out, l.InstanceType)
	}
	return out
}
