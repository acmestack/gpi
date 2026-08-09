package task

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Accelerators maps an accelerator name (e.g. "A100") to the requested count.
type Accelerators map[string]int

// String renders the accelerators as a sorted "A100:4,T4" style summary.
func (a Accelerators) String() string {
	if len(a) == 0 {
		return ""
	}
	names := make([]string, 0, len(a))
	for name := range a {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		if a[name] == 1 {
			parts = append(parts, name)
		} else {
			parts = append(parts, fmt.Sprintf("%s:%d", name, a[name]))
		}
	}
	return strings.Join(parts, ",")
}

// ParseAccelerators parses accelerators from a string ("A100:4"), a []any, or
// a map[string]any. Returns nil for nil input.
func ParseAccelerators(spec any) (Accelerators, error) {
	result := Accelerators{}
	switch v := spec.(type) {
	case nil:
		return nil, nil
	case string:
		spec = strings.Split(v, ",")
	}
	switch v := spec.(type) {
	case []any:
		for _, item := range v {
			if err := addAccelerator(result, fmt.Sprintf("%v", item), false); err != nil {
				return nil, err
			}
		}
	case []string:
		for _, item := range v {
			if err := addAccelerator(result, item, false); err != nil {
				return nil, err
			}
		}
	case map[any]any:
		for k, val := range v {
			count, ok := val.(int)
			if !ok {
				return nil, fmt.Errorf("invalid accelerator count %v for %v", val, k)
			}
			result[fmt.Sprintf("%v", k)] = count
		}
	case map[string]any:
		for k, val := range v {
			switch count := val.(type) {
			case int:
				result[k] = count
			case float64:
				result[k] = int(count)
			default:
				return nil, fmt.Errorf("invalid accelerator count %v for %v", val, k)
			}
		}
	default:
		return nil, fmt.Errorf("accelerators must be a string (e.g. \"A100:4\"), a list, or a map, got %T", spec)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func addAccelerator(result Accelerators, item string, noCount bool) error {
	item = strings.TrimSpace(item)
	if item == "" {
		return nil
	}
	parts := strings.SplitN(item, ":", 2)
	if noCount || len(parts) == 1 {
		result[item] = 1
		return nil
	}
	name := strings.TrimSpace(parts[0])
	pieces := strings.SplitN(parts[1], ":", 2)
	count, err := strconv.Atoi(strings.TrimSpace(pieces[0]))
	if err != nil || count <= 0 {
		count = 1
	}
	result[name] = count
	return nil
}
