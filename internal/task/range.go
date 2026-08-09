package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Range is a numeric range with optional min/max, parsed from strings like
// "8", "8+", "-8" or "4-8".
type Range struct {
	Min *float64
	Max *float64
}

// ParseRange parses a range expression; empty input yields nil.
func ParseRange(s string) (*Range, error) {
	if s == "" {
		return nil, nil
	}
	s = strings.TrimSpace(s)
	lower := false
	upper := false
	switch {
	case strings.HasSuffix(s, "+"):
		lower = true
		s = strings.TrimSuffix(s, "+")
	case strings.HasPrefix(s, "-"):
		upper = true
		s = strings.TrimPrefix(s, "-")
	}
	parts := strings.SplitN(s, "-", 2)
	if len(parts) == 1 {
		v, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid range value %q: %w", s, err)
		}
		r := &Range{}
		if lower {
			r.Min = &v
		} else if upper {
			r.Max = &v
		} else {
			r.Min = &v
			r.Max = &v
		}
		return r, nil
	}
	if strings.TrimSpace(parts[0]) == "" {
		upper = true
	}
	if strings.TrimSpace(parts[1]) == "" {
		lower = true
	}
	r := &Range{}
	if !lower {
		v, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid range lower bound %q: %w", parts[0], err)
		}
		r.Min = &v
	}
	if !upper {
		v, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid range upper bound %q: %w", parts[1], err)
		}
		r.Max = &v
	}
	return r, nil
}

// Matches reports whether the given value falls within the range bounds.
func (r *Range) Matches(v float64) bool {
	if r == nil {
		return true
	}
	if r.Min != nil && v < *r.Min {
		return false
	}
	if r.Max != nil && v > *r.Max {
		return false
	}
	return true
}

// String renders the range back into its "4", "4+", "-8" or "4-8" form.
func (r *Range) String() string {
	if r == nil {
		return ""
	}
	switch {
	case r.Min != nil && r.Max != nil:
		if *r.Min == *r.Max {
			return fmt.Sprintf("%g", *r.Min)
		}
		return fmt.Sprintf("%g-%g", *r.Min, *r.Max)
	case r.Min != nil:
		return fmt.Sprintf("%g+", *r.Min)
	case r.Max != nil:
		return fmt.Sprintf("-%g", *r.Max)
	}
	return ""
}

// UnmarshalYAML parses a number or range string (e.g. "4", "4+", "4-8").
func (r *Range) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		parsed, err := ParseRange(s)
		if err != nil {
			return err
		}
		if parsed == nil {
			return nil
		}
		*r = *parsed
		return nil
	}
	var f float64
	if err := unmarshal(&f); err == nil {
		v := f
		*r = Range{Min: &v, Max: &v}
		return nil
	}
	return errors.New("range must be a number or a range string like 4+, 4-8")
}

// UnmarshalJSON parses a number or range string from a JSON task body (e.g.
// `"cpus": "8+"` or `"cpus": 8`), mirroring UnmarshalYAML.
func (r *Range) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		parsed, err := ParseRange(s)
		if err != nil {
			return err
		}
		if parsed == nil {
			return nil
		}
		*r = *parsed
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		v := f
		*r = Range{Min: &v, Max: &v}
		return nil
	}
	return errors.New("range must be a number or a range string like 4+, 4-8")
}
