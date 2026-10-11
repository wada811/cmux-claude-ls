package main

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWidth(t *testing.T) {
	for _, c := range []struct {
		s    string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"日本語", 6},
		{"あいうカナ", 10},
		{"한글", 4},
		{"ＡＢ", 4},
		{"a日b", 4},
		{"✳ x", 3},
		{"…", 1},
	} {
		if got := width(c.s); got != c.want {
			t.Errorf("width(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestFit(t *testing.T) {
	for _, c := range []struct {
		s    string
		w    int
		want string
	}{
		{"abc", 5, "abc  "},
		{"abcde", 5, "abcde"},
		{"abcdef", 5, "abcd…"},
		{"日本語", 8, "日本語  "},
		{"日本語テキスト", 8, "日本語… "}, // 6 cols + "…" = 7, padded to 8
		{"a  b\tc", 6, "a b c "},
		{"", 3, "   "},
	} {
		got := fit(c.s, c.w)
		if got != c.want {
			t.Errorf("fit(%q, %d) = %q, want %q", c.s, c.w, got, c.want)
		}
		if width(got) != c.w {
			t.Errorf("fit(%q, %d) width = %d", c.s, c.w, width(got))
		}
	}
}

func TestAgo(t *testing.T) {
	now := time.Unix(1_000_000_000, 0)
	at := func(sec int64) *int64 { v := now.Unix() - sec; return &v }
	for _, c := range []struct {
		ts   *int64
		want string
	}{
		{nil, "-"},
		{at(0), "now"},
		{at(59), "now"},
		{at(60), "1m"},
		{at(3599), "59m"},
		{at(3600), "1h"},
		{at(86399), "23h"},
		{at(86400), "1d"},
		{at(86400 * 5), "5d"},
		{at(-30), "now"},
	} {
		if got := ago(c.ts, now); got != c.want {
			t.Errorf("ago(%v) = %q, want %q", c.ts, got, c.want)
		}
	}
}

func TestEmitExitStatus(t *testing.T) {
	for _, c := range []struct {
		name     string
		rows     []*Row
		json     bool
		wantCode int
		wantOut  string
	}{
		{"table rows", testRows()[:1], false, 0, "SURFACE"},
		{"table empty", []*Row{}, false, 1, "SURFACE"},
		{"json rows", testRows()[:1], true, 0, `"state": "busy"`},
		{"json empty", []*Row{}, true, 1, "[]"},
	} {
		var out bytes.Buffer
		cols := fields
		if !c.json {
			cols, _ = pickFields(strings.Join(tableFields, ","))
		}
		code := emit(&out, c.rows, c.json, cols)
		if code != c.wantCode || !strings.Contains(out.String(), c.wantOut) {
			t.Errorf("%s: code=%d out=%q", c.name, code, out.String())
		}
	}
}

func TestJSONKeys(t *testing.T) {
	s := "x"
	var out bytes.Buffer
	emit(&out, []*Row{{WindowRef: &s, WorkspaceRef: &s, WorkspaceTitle: &s, SurfaceRef: &s, SurfaceTitle: &s,
		State: "idle", Cwd: &s, SessionID: "id", TranscriptPath: &s}}, true, fields)
	var got []json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || len(got) != 1 {
		t.Fatalf("bad json: %v %q", err, out.String())
	}
	// Keys must appear in this order.
	want := []string{"window_ref", "workspace_ref", "workspace_title", "surface_ref", "surface_title", "state",
		"waiting_for", "updated", "age", "cwd", "session_id", "pid", "transcript_path"}
	pos := -1
	for _, k := range want {
		i := strings.Index(out.String(), `"`+k+`":`)
		if i <= pos {
			t.Errorf("key %q missing or out of order in %q", k, out.String())
		}
		pos = i
	}
	for _, old := range []string{`"title"`, `"session"`, `"jsonl"`} {
		if strings.Contains(out.String(), old) {
			t.Errorf("old key %s still present", old)
		}
	}
}

func TestFields(t *testing.T) {
	cols, err := pickFields("surface_ref, STATE,age")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	printTable(&out, testRows()[:1], cols)
	header := strings.SplitN(out.String(), "\n", 2)[0]
	if got := strings.Fields(header); !slices.Equal(got, []string{"SURFACE_REF", "STATE", "AGE"}) {
		t.Errorf("header = %q", header)
	}
	out.Reset()
	printJSON(&out, testRows()[:1], cols)
	var got []map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || len(got) != 1 || len(got[0]) != 3 {
		t.Errorf("json = %q", out.String())
	}
	for _, bad := range []string{"", "state,", "title", "surface"} {
		if _, err := pickFields(bad); err == nil {
			t.Errorf("pickFields(%q): want error", bad)
		}
	}
	var stderr bytes.Buffer
	if code := run([]string{"--fields", "nope"}, io.Discard, &stderr); code != 2 {
		t.Errorf("unknown field: code=%d", code)
	}
}

func TestFieldForms(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	ts := int64(1_000_000 - 3*3600)
	pid := 1234567
	r := &Row{Updated: &ts, Pid: &pid}
	get := func(name string) field { return fields[slices.Index(fieldNames(), name)] }
	if v := get("age").value(r, now); v != int64(3*3600) {
		t.Errorf("age value = %v", v)
	}
	if v := get("age").text(r, now); v != "3h" {
		t.Errorf("age text = %q", v)
	}
	if v := get("pid").text(r, now); v != "1234567" {
		t.Errorf("pid text = %q", v)
	}
	if v := get("age").value(&Row{}, now); v != nil {
		t.Errorf("age without updated = %v", v)
	}
	for _, c := range []struct {
		in   WaitingFor
		want string
	}{
		{nil, "-"},
		{waitUnknown{"unknown"}, "unknown"},
		{waitPermission{"permission", "Bash", "go test"}, "permission: Bash go test"},
		{waitQuestions{"question", []waitQuestion{{Question: "q1", Header: "h1"}, {Question: "q2"}}}, "question: h1 (+1)"},
		{waitQuestions{"question", []waitQuestion{{Question: "q1"}}}, "question: q1"},
	} {
		if got := waitingText(c.in); got != c.want {
			t.Errorf("waitingText(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
