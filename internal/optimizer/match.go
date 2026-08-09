package optimizer

import (
	"strings"

	"github.com/acmestack/gpi/internal/cloud/catalog"
	"github.com/acmestack/gpi/internal/task"
)

// matchesResources reports whether a cloud instance spec satisfies the task's
// resource requirements (instance type, CPU/memory/disk ranges, accelerators).
// It is used by collectCandidates to filter the feasible candidate set.
func matchesResources(c *catalog.Instance, rs *task.Resources) bool {
	if rs.InstanceType != "" && c.InstanceType != rs.InstanceType {
		return false
	}
	if rs.Cpus != nil && !rs.Cpus.Matches(float64(c.VCPUs)) {
		return false
	}
	if rs.Memory != nil && !rs.Memory.Matches(c.MemoryGiB) {
		return false
	}
	if rs.DiskSize != nil {
		requested := rs.DiskSize.Min
		if requested == nil {
			requested = rs.DiskSize.Max
		}
		if requested != nil && c.MaxDiskGiB < *requested {
			return false
		}
	}
	for name, count := range rs.Accelerators {
		n, ok := c.Accelerators[strings.ToLower(name)]
		if !ok {
			n, ok = c.Accelerators[name]
		}
		if !ok || n < count {
			return false
		}
	}
	return true
}
