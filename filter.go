// Filters and sort order of the listed rows.

package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// matchWorkspace reports whether a row is in the workspace ws, given the same
// forms as cmux --workspace: a ref (workspace:3), a UUID, or a bare number.
// A bare number is the workspace index, counted per window, so it is looked up
// in the caller's window when cmux reported one.
func matchWorkspace(r *Row, ws string, callerWindow string) bool {
	if n, err := strconv.Atoi(ws); err == nil && n >= 0 {
		return r.wsIndex == n && (callerWindow == "" || deref(r.Window) == callerWindow)
	}
	return deref(r.Workspace) == ws || (r.wsID != "" && strings.EqualFold(r.wsID, ws))
}

// filterWorkspace keeps the rows in the workspace ws ("" keeps all rows).
func filterWorkspace(rows []*Row, ws string, callerWindow string) []*Row {
	if ws == "" {
		return rows
	}
	out := []*Row{}
	for _, r := range rows {
		if matchWorkspace(r, ws, callerWindow) {
			out = append(out, r)
		}
	}
	return out
}

// excludeSurface drops the row of the given surface ref ("" keeps all rows).
func excludeSurface(rows []*Row, ref string) []*Row {
	if ref == "" {
		return rows
	}
	out := []*Row{}
	for _, r := range rows {
		if deref(r.Surface) != ref {
			out = append(out, r)
		}
	}
	return out
}

var validStates = []string{"busy", "waiting", "idle", "shell", "stopped"}

// parseStates parses --state: a comma-separated list of known states.
func parseStates(s string) (map[string]bool, error) {
	set := map[string]bool{}
	for _, v := range strings.Split(s, ",") {
		v = strings.TrimSpace(v)
		ok := false
		for _, k := range validStates {
			ok = ok || v == k
		}
		if !ok {
			return nil, fmt.Errorf("invalid --state value %q (use a comma-separated list of %s)",
				v, strings.Join(validStates, ", "))
		}
		set[v] = true
	}
	return set, nil
}

// parseAge parses --older-than: a Go duration, or whole days like "14d".
func parseAge(s string) (time.Duration, error) {
	bad := fmt.Errorf("invalid --older-than value %q (examples: 30m, 12h, 14d)", s)
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.ParseInt(n, 10, 64)
		if err != nil || days < 0 || days > math.MaxInt64/int64(24*time.Hour) {
			return 0, bad
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, bad
	}
	return d, nil
}

// filterRows keeps rows whose state is in states (nil means any) and whose
// last message is older than olderThan (nil means any). With olderThan set,
// rows without a last message are dropped.
func filterRows(rows []*Row, states map[string]bool, olderThan *time.Duration, now time.Time) []*Row {
	out := []*Row{}
	for _, r := range rows {
		if states != nil && !states[fmt.Sprint(r.State)] {
			continue
		}
		if olderThan != nil {
			if r.Updated == nil || now.Unix()-*r.Updated <= int64(olderThan.Seconds()) {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

var stateRank = map[string]int{"waiting": 0, "idle": 1, "shell": 2, "busy": 3, "stopped": 4}

// sortRows reorders rows in place. "tree" keeps the given order. Ties keep
// the tree order (stable sort).
func sortRows(rows []*Row, order string) {
	newer := func(a, b *Row) bool { // a is strictly newer; nil is oldest
		switch {
		case a.Updated == nil:
			return false
		case b.Updated == nil:
			return true
		}
		return *a.Updated > *b.Updated
	}
	rank := func(r *Row) int {
		if n, ok := stateRank[fmt.Sprint(r.State)]; ok {
			return n
		}
		return len(stateRank) // unknown states go last
	}
	switch order {
	case "state":
		sort.SliceStable(rows, func(i, j int) bool {
			if ri, rj := rank(rows[i]), rank(rows[j]); ri != rj {
				return ri < rj
			}
			return newer(rows[i], rows[j])
		})
	case "age":
		sort.SliceStable(rows, func(i, j int) bool { return newer(rows[i], rows[j]) })
	}
}
