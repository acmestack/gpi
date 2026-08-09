//go:build linux || darwin

package gpilet

import "syscall"

// diskUsage reports root filesystem usage via syscall.Statfs. The metric is
// best-effort: on failure it returns zeros rather than erroring.
func diskUsage() (totalGB, usedGB float64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0, 0
	}
	total := float64(stat.Blocks) * float64(stat.Bsize) / 1e9
	free := float64(stat.Bfree) * float64(stat.Bsize) / 1e9
	return total, total - free
}
