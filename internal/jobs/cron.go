package jobs

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type schedule struct {
	minute, hour, dom, month, dow []int
}

func parseSchedule(spec string) (*schedule, time.Duration, error) {
	if spec == "" {
		return nil, 0, fmt.Errorf("empty schedule")
	}
	if strings.HasPrefix(spec, "@every ") {
		d, err := time.ParseDuration(strings.TrimSpace(spec[len("@every "):]))
		if err != nil {
			return nil, 0, fmt.Errorf("invalid @every duration: %w", err)
		}
		return nil, d, nil
	}
	translate := map[string]string{
		"@minutely": "* * * * *",
		"@hourly":   "0 * * * *",
		"@daily":    "0 0 * * *",
		"@midnight": "0 0 * * *",
		"@weekly":   "0 0 * * 0",
		"@monthly":  "0 0 1 * *",
	}
	if t, ok := translate[spec]; ok {
		spec = t
	}
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return nil, 0, fmt.Errorf("cron spec must have 5 fields, got %d", len(fields))
	}
	parsers := []func(string) ([]int, error){
		parseField(minuteRange),
		parseField(hourRange),
		parseField(dayRange),
		parseField(monthRange),
		parseField(dowRange),
	}
	s := &schedule{}
	for i, p := range parsers {
		vals, err := p(fields[i])
		if err != nil {
			return nil, 0, fmt.Errorf("field %d: %w", i, err)
		}
		switch i {
		case 0:
			s.minute = vals
		case 1:
			s.hour = vals
		case 2:
			s.dom = vals
		case 3:
			s.month = vals
		case 4:
			s.dow = vals
		}
	}
	return s, 0, nil
}

func minuteRange(n int) bool { return n >= 0 && n <= 59 }
func hourRange(n int) bool   { return n >= 0 && n <= 23 }
func dayRange(n int) bool    { return n >= 1 && n <= 31 }
func monthRange(n int) bool  { return n >= 1 && n <= 12 }
func dowRange(n int) bool    { return n >= 0 && n <= 6 }

func parseField(valid func(int) bool) func(string) ([]int, error) {
	return func(field string) ([]int, error) {
		if field == "*" {
			return nil, nil
		}
		vals := []int{}
		for _, part := range strings.Split(field, ",") {
			step := 1
			rangePart := part
			if idx := strings.Index(part, "/"); idx >= 0 {
				stepStr := part[idx+1:]
				rangePart = part[:idx]
				var err error
				step, err = strconv.Atoi(stepStr)
				if err != nil || step < 1 {
					return nil, fmt.Errorf("invalid step in %q", part)
				}
			}
			start := 0
			end := 59
			if rangePart != "*" {
				if strings.Contains(rangePart, "-") {
					se := strings.SplitN(rangePart, "-", 2)
					s, err1 := strconv.Atoi(se[0])
					e, err2 := strconv.Atoi(se[1])
					if err1 != nil || err2 != nil {
						return nil, fmt.Errorf("invalid range %q", rangePart)
					}
					start, end = s, e
				} else {
					v, err := strconv.Atoi(rangePart)
					if err != nil {
						return nil, fmt.Errorf("invalid value %q", rangePart)
					}
					start, end = v, v
				}
			}
			for v := start; v <= end; v += step {
				if !valid(v) {
					return nil, fmt.Errorf("value %d out of range", v)
				}
				vals = append(vals, v)
			}
		}
		return vals, nil
	}
}

func (s *schedule) next(from time.Time) time.Time {
	candidate := from.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 5*365*24*60; i++ {
		if s.match(candidate) {
			return candidate
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}
}

func (s *schedule) match(t time.Time) bool {
	if !contains(s.minute, t.Minute()) {
		return false
	}
	if !contains(s.hour, t.Hour()) {
		return false
	}
	if !contains(s.month, int(t.Month())) {
		return false
	}
	if !contains(s.dow, int(t.Weekday())) {
		return false
	}
	return contains(s.dom, t.Day())
}

func contains(vals []int, v int) bool {
	if vals == nil {
		return true
	}
	for _, x := range vals {
		if x == v {
			return true
		}
	}
	return false
}
