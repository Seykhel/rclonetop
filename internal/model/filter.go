package model

import (
	"strings"
	"unicode"
)

// Filter projects the selectable subjects without changing the resolved facts.
// Host context and source health remain visible even when no subject matches.
// Like View itself, the returned rows and context are read-only; the subject
// slices are separate so selecting a subset never overwrites the full view.
func (v View) Filter(query string) View {
	query = foldFilterText(strings.TrimSpace(query))
	out := v
	out.Procs = make([]ProcRow, 0, len(v.Procs))
	out.Units = make([]UnitRow, 0, len(v.Units))
	for _, row := range v.Procs {
		fields := []string{string(row.Process.Kind), row.Process.Unit, row.Process.Target, row.Unit.Name, row.Timer.Name, row.Unit.LogFile, row.Job.LogFile, string(row.Job.Kind), row.Job.Path1, row.Job.Path2}
		fields = append(fields, row.Process.Paths...)
		fields = append(fields, row.Process.Remotes...)
		if matchesFilter(query, fields...) {
			out.Procs = append(out.Procs, row)
		}
	}
	for _, row := range v.Units {
		if matchesFilter(query, row.Unit.Name, row.Timer.Name, row.Unit.LogFile, row.Job.LogFile, string(row.Job.Kind), row.Job.Path1, row.Job.Path2) {
			out.Units = append(out.Units, row)
		}
	}
	return out
}

func matchesFilter(query string, fields ...string) bool {
	if query == "" {
		return true
	}
	for _, field := range fields {
		if strings.Contains(foldFilterText(field), query) {
			return true
		}
	}
	return false
}

// SimpleFold includes Unicode case partners lowercasing alone misses, such as
// Greek final sigma. A canonical rune per cycle also preserves literal spaces.
func foldFilterText(text string) string {
	return strings.Map(func(r rune) rune {
		canonical := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < canonical {
				canonical = next
			}
		}
		return canonical
	}, text)
}
