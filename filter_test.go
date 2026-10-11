package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseAge(t *testing.T) {
	for _, c := range []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"14d", 14 * 24 * time.Hour, false},
		{"0d", 0, false},
		{"12h", 12 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"1h30m", 90 * time.Minute, false},
		{"1.5d", 0, true},
		{"-1d", 0, true},
		{"-1h", 0, true},
		{"d", 0, true},
		{"abc", 0, true},
		{"14", 0, true},
		{"", 0, true},
		{"99999999999d", 0, true},
	} {
		got, err := parseAge(c.in)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("parseAge(%q) = %v, %v; want %v, err=%v", c.in, got, err, c.want, c.wantErr)
		}
	}
}

func TestParseStates(t *testing.T) {
	for _, c := range []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"waiting", 1, false},
		{"waiting,idle", 2, false},
		{"waiting, idle", 2, false},
		{"busy,waiting,idle,shell,stopped", 5, false},
		{"foo", 0, true},
		{"waiting,foo", 0, true},
		{"", 0, true},
		{"waiting,", 0, true},
		{"Waiting", 0, true},
	} {
		got, err := parseStates(c.in)
		if (err != nil) != c.wantErr || len(got) != c.want {
			t.Errorf("parseStates(%q) = %v, %v", c.in, got, err)
		}
	}
}

func testRows() []*Row {
	mk := func(surface, state string, updated *int64) *Row {
		return &Row{Surface: &surface, State: state, Updated: updated}
	}
	at := func(n int64) *int64 { return &n }
	return []*Row{
		mk("s1", "busy", at(900)),
		mk("s2", "stopped", at(100)),
		mk("s3", "idle", at(500)),
		mk("s4", "waiting", nil),
		mk("s5", "waiting", at(300)),
		mk("s6", "idle", at(700)),
		mk("s7", "shell", at(800)),
		mk("s8", "stopped", nil),
	}
}

func surfaces(rows []*Row) string {
	var s []string
	for _, r := range rows {
		s = append(s, *r.Surface)
	}
	return strings.Join(s, ",")
}

func TestFilterRows(t *testing.T) {
	now := time.Unix(1000, 0)
	age := func(d time.Duration) *time.Duration { return &d }
	for _, c := range []struct {
		name   string
		states map[string]bool
		older  *time.Duration
		want   string
	}{
		{"none", nil, nil, "s1,s2,s3,s4,s5,s6,s7,s8"},
		{"state", map[string]bool{"waiting": true, "idle": true}, nil, "s3,s4,s5,s6"},
		{"older drops null", nil, age(600 * time.Second), "s2,s5"},
		{"older boundary is exclusive", nil, age(700 * time.Second), "s2"},
		{"both", map[string]bool{"stopped": true}, age(600 * time.Second), "s2"},
		{"no match", map[string]bool{"busy": true}, age(2000 * time.Second), ""},
	} {
		if got := surfaces(filterRows(testRows(), c.states, c.older, now)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSortRows(t *testing.T) {
	for _, c := range []struct{ order, want string }{
		{"tree", "s1,s2,s3,s4,s5,s6,s7,s8"},
		{"state", "s5,s4,s6,s3,s7,s1,s2,s8"},
		{"age", "s1,s7,s6,s3,s5,s2,s4,s8"},
	} {
		rows := testRows()
		sortRows(rows, c.order)
		if got := surfaces(rows); got != c.want {
			t.Errorf("sort %s: got %q, want %q", c.order, got, c.want)
		}
	}
}
