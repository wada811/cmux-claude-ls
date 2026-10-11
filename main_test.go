package main

import (
	"bytes"
	"encoding/json"
	"errors"
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

func TestParseTree(t *testing.T) {
	for _, c := range []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{"ok", `{"windows":[{"ref":"window:1","workspaces":[{"ref":"workspace:1","title":"w","panes":[{"surfaces":[
			{"ref":"surface:1","title":"a","type":"terminal"},{"ref":"surface:2","type":"browser"}]}]}]}]}`, 1, false},
		{"no windows key", `{"active":{}}`, 0, true},
		{"windows null", `{"windows":null}`, 0, true},
		{"empty windows", `{"windows":[]}`, 0, true},
		{"no terminal", `{"windows":[{"workspaces":[{"panes":[{"surfaces":[{"type":"browser"}]}]}]}]}`, 0, true},
		{"broken json", `{"windows":`, 0, true},
		{"not an object", `[]`, 0, true},
		{"empty", ``, 0, true},
	} {
		rows, err := parseTree([]byte(c.in))
		if (err != nil) != c.wantErr || len(rows) != c.want {
			t.Errorf("%s: got %d rows, err=%v", c.name, len(rows), err)
		}
	}
}

func TestParseRestoreRecord(t *testing.T) {
	for _, c := range []struct {
		name         string
		in           string
		runErr       error
		wantRec      bool
		wantReadable bool
	}{
		{"claude", `{"restore_record":{"kind":"claude","checkpoint_id":"abc"}}`, nil, true, true},
		{"null record", `{"restore_record":null,"surface_ref":"surface:1"}`, nil, false, true},
		{"other kind", `{"restore_record":{"kind":"codex","checkpoint_id":""}}`, nil, false, true},
		{"claude without id", `{"restore_record":{"kind":"claude","checkpoint_id":""}}`, nil, false, false},
		{"claude missing id", `{"restore_record":{"kind":"claude"}}`, nil, false, false},
		{"key missing", `{"surface_ref":"surface:1"}`, nil, false, false},
		{"broken json", `{"restore_record":`, nil, false, false},
		{"not an object", `[]`, nil, false, false},
		{"record not an object", `{"restore_record":"x"}`, nil, false, false},
		{"empty output", ``, nil, false, false},
		{"command failed", `{"restore_record":null}`, errors.New("exit status 1"), false, false},
	} {
		rec, ok := parseRestoreRecord([]byte(c.in), c.runErr)
		if (rec != nil) != c.wantRec || ok != c.wantReadable {
			t.Errorf("%s: rec=%v readable=%v, want rec=%v readable=%v", c.name, rec, ok, c.wantRec, c.wantReadable)
		}
	}
}

func TestJudgeRecords(t *testing.T) {
	for _, c := range []struct {
		total, unreadable int
		want              recordVerdict
	}{
		{5, 0, recordsOK},
		{5, 1, recordsWarn},
		{5, 4, recordsWarn},
		{5, 5, recordsFail},
		{1, 1, recordsFail},
	} {
		if got := judgeRecords(c.total, c.unreadable); got != c.want {
			t.Errorf("judgeRecords(%d, %d) = %d, want %d", c.total, c.unreadable, got, c.want)
		}
	}
}

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

func TestFormatError(t *testing.T) {
	const tail = "\nPlease report it at https://github.com/wada811/cmux-list-claude-sessions/issues"
	for _, c := range []struct{ ver, want string }{
		{"0.64.22 (102) [abc]", "cmux-list-claude-sessions: x (cmux 0.64.22 (102) [abc] ; the output format may have changed)" + tail},
		{"", "cmux-list-claude-sessions: x (the output format may have changed)" + tail},
	} {
		if got := formatError("x", c.ver); got != c.want {
			t.Errorf("formatError(%q) = %q, want %q", c.ver, got, c.want)
		}
	}
}

func TestRunFlags(t *testing.T) {
	for _, c := range []struct {
		name         string
		args         []string
		wantCode     int
		wantOut, err string
	}{
		{"help", []string{"--help"}, 0, "Options:", ""},
		{"-h", []string{"-h"}, 0, "Options:", ""},
		{"version", []string{"--version"}, 0, "cmux-list-claude-sessions ", ""},
		{"unknown flag", []string{"--bogus"}, 2, "", "flag provided but not defined: -bogus"},
		{"positional", []string{"foo"}, 2, "", "unexpected argument: foo"},
	} {
		var out, errb bytes.Buffer
		code := run(c.args, &out, &errb)
		if code != c.wantCode || !strings.Contains(out.String(), c.wantOut) || !strings.Contains(errb.String(), c.err) {
			t.Errorf("%s: code=%d out=%q err=%q", c.name, code, out.String(), errb.String())
		}
		if c.wantCode == 2 && !strings.Contains(errb.String(), "usage: cmux-list-claude-sessions") {
			t.Errorf("%s: no usage on stderr: %q", c.name, errb.String())
		}
	}
}

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
		code := emit(&out, c.rows, c.json)
		if code != c.wantCode || !strings.Contains(out.String(), c.wantOut) {
			t.Errorf("%s: code=%d out=%q", c.name, code, out.String())
		}
	}
}

func TestJSONKeys(t *testing.T) {
	s := "x"
	var out bytes.Buffer
	emit(&out, []*Row{{Window: &s, Workspace: &s, WorkspaceTitle: &s, Surface: &s, SurfaceTitle: &s,
		State: "idle", Cwd: &s, SessionID: "id", TranscriptPath: &s}}, true)
	var got []json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || len(got) != 1 {
		t.Fatalf("bad json: %v %q", err, out.String())
	}
	// Keys must appear in this order.
	want := []string{"window", "workspace", "workspace_title", "surface", "surface_title", "state",
		"updated", "cwd", "session_id", "pid", "transcript_path"}
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

func TestRunUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--state", "foo"},
		{"--state", ""},
		{"--state", "waiting,foo"},
		{"--older-than", "abc"},
		{"--older-than", "-1d"},
		{"--sort", "foo"},
		{"--sort", ""},
	} {
		var out, errb bytes.Buffer
		code := run(args, &out, &errb)
		if code != 2 || out.Len() != 0 || !strings.Contains(errb.String(), "usage: cmux-list-claude-sessions") ||
			!strings.Contains(errb.String(), "Run 'cmux-list-claude-sessions --help' for details.") {
			t.Errorf("%v: code=%d out=%q err=%q", args, code, out.String(), errb.String())
		}
	}
}
