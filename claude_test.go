package main

import (
	"testing"
)

func TestParseSessionFile(t *testing.T) {
	for _, c := range []struct {
		name string
		in   string
		want bool
	}{
		{"ok", `{"pid":123,"sessionId":"abc","status":"idle"}`, true},
		{"pid as string", `{"pid":"123","sessionId":"abc"}`, true},
		{"no pid", `{"sessionId":"abc"}`, false},
		{"no session id", `{"pid":123}`, false},
		{"empty session id", `{"pid":123,"sessionId":""}`, false},
		{"broken", `{"pid":`, false},
		{"null", `null`, false},
		{"array", `[]`, false},
	} {
		_, _, _, ok := parseSessionFile([]byte(c.in))
		if ok != c.want {
			t.Errorf("%s: ok=%v, want %v", c.name, ok, c.want)
		}
	}
}

func TestSessionsUnreadable(t *testing.T) {
	for _, c := range []struct {
		total, readable int
		want            bool
	}{
		{0, 0, false},
		{3, 0, true},
		{3, 1, false},
		{3, 3, false},
	} {
		if got := sessionsUnreadable(c.total, c.readable); got != c.want {
			t.Errorf("sessionsUnreadable(%d, %d) = %v, want %v", c.total, c.readable, got, c.want)
		}
	}
}
