package srs

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	maxSteps = 30
	minStep  = time.Minute
	maxStep  = 30 * 24 * time.Hour
)

// ParseSteps reads learning steps like "1m 10m 1h 1d" (space or comma
// separated; m, h and d units; at most 30 steps, each 1 minute to 30 days).
// An empty string is no steps.
func ParseSteps(s string) ([]time.Duration, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' })
	if len(fields) > maxSteps {
		return nil, errors.New("at most 30 steps")
	}
	out := []time.Duration{}
	for _, f := range fields {
		f = strings.ToLower(strings.TrimSpace(f))
		if len(f) < 2 {
			return nil, fmt.Errorf("%q is not a step (use 1m, 10m, 1h, 1d)", f)
		}
		unit := map[byte]time.Duration{'m': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour}[f[len(f)-1]]
		if unit == 0 {
			return nil, fmt.Errorf("%q is not a step (use 1m, 10m, 1h, 1d)", f)
		}
		n, err := strconv.ParseFloat(f[:len(f)-1], 64)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%q is not a step (use 1m, 10m, 1h, 1d)", f)
		}
		d := time.Duration(n * float64(unit)).Round(time.Second)
		if d < minStep || d > maxStep {
			return nil, fmt.Errorf("%q: each step is 1 minute to 30 days", f)
		}
		out = append(out, d)
	}
	return out, nil
}

// FormatSteps writes steps the way ParseSteps reads them ("1m 10m").
func FormatSteps(d []time.Duration) string {
	parts := make([]string, 0, len(d))
	for _, x := range d {
		switch {
		case x%(24*time.Hour) == 0:
			parts = append(parts, fmt.Sprintf("%dd", x/(24*time.Hour)))
		case x%time.Hour == 0:
			parts = append(parts, fmt.Sprintf("%dh", x/time.Hour))
		case x%time.Minute == 0:
			parts = append(parts, fmt.Sprintf("%dm", x/time.Minute))
		default:
			parts = append(parts, strconv.FormatFloat(x.Minutes(), 'f', -1, 64)+"m")
		}
	}
	return strings.Join(parts, " ")
}
