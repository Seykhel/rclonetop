// Package preset parses the compact box:P:G layout notation.
package preset

import (
	"fmt"
	"strconv"
	"strings"
)

// Entry is one panel placement in a framed preset.
type Entry struct {
	Box    string
	Column int
	Weight int
}

// Parse validates and parses a space-separated preset value.
func Parse(raw string) ([]Entry, error) {
	var out []Entry
	seen := map[string]bool{}
	for _, token := range strings.Fields(raw) {
		parts := strings.Split(token, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("preset entry %q must be box:column:weight", token)
		}
		switch parts[0] {
		case "transfers", "bandwidth", "files", "status":
		default:
			return nil, fmt.Errorf("unknown box %q", parts[0])
		}
		if seen[parts[0]] {
			return nil, fmt.Errorf("box %q appears more than once", parts[0])
		}
		column, err := strconv.Atoi(parts[1])
		if err != nil || (column != 0 && column != 1) {
			return nil, fmt.Errorf("box %q column must be 0 or 1", parts[0])
		}
		weight, err := strconv.Atoi(parts[2])
		if err != nil || weight <= 0 {
			return nil, fmt.Errorf("box %q weight must be a positive integer", parts[0])
		}
		seen[parts[0]] = true
		out = append(out, Entry{Box: parts[0], Column: column, Weight: weight})
	}
	return out, nil
}
