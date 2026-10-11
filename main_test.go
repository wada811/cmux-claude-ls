package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
		rows, _, err := parseTree([]byte(c.in))
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
		"waiting_for", "updated", "cwd", "session_id", "pid", "transcript_path"}
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
		{"--workspace", ""},
		{"--workspace", "  "},
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

func TestParseWaitingFor(t *testing.T) {
	ask := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"AskUserQuestion","input":{"questions":[{"question":"Pick a fruit?","header":"Fruit","multiSelect":true,"options":[{"label":"Apple","description":"red"},{"label":"Pear"}]}]}}]}}`
	bash := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"echo  hello\nworld","description":"x"}}]}}`
	edit := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t3","name":"Edit","input":{"file_path":"/tmp/a.txt","old_string":"a","new_string":"b"}}]}}`
	other := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t4","name":"Grep","input":{"pattern":"foo"}}]}}`
	res := func(id string) string {
		return `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"` + id + `","content":"ok"}]}}`
	}
	meta := `{"type":"last-prompt"}`
	for _, c := range []struct {
		name  string
		lines []string
		want  string
	}{
		{"question", []string{bash, res("t2"), ask, meta},
			`{"kind":"question","questions":[{"question":"Pick a fruit?","header":"Fruit","options":["Apple","Pear"],"multi_select":true}]}`},
		{"permission bash", []string{ask, res("t1"), bash, meta},
			`{"kind":"permission","tool":"Bash","input":"echo hello world"}`},
		{"permission edit path", []string{edit}, `{"kind":"permission","tool":"Edit","input":"/tmp/a.txt"}`},
		{"permission other as json", []string{other}, `{"kind":"permission","tool":"Grep","input":"{\"pattern\":\"foo\"}"}`},
		{"resolved only", []string{bash, res("t2"), meta}, `{"kind":"unknown"}`},
		{"empty", nil, `{"kind":"unknown"}`},
		{"broken lines skipped", []string{`{"type":"assistant","mess`, ask, `not json`, meta},
			`{"kind":"question","questions":[{"question":"Pick a fruit?","header":"Fruit","options":["Apple","Pear"],"multi_select":true}]}`},
		{"cut-off first line", []string{`ssage":{"content":[]}}`, edit}, `{"kind":"permission","tool":"Edit","input":"/tmp/a.txt"}`},
		{"question without questions", []string{`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t9","name":"AskUserQuestion","input":{}}]}}`},
			`{"kind":"unknown"}`},
		{"sidechain ignored", []string{ask, `{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"tool_use","id":"s1","name":"Bash","input":{"command":"x"}}]}}`},
			`{"kind":"question","questions":[{"question":"Pick a fruit?","header":"Fruit","options":["Apple","Pear"],"multi_select":true}]}`},
	} {
		got, _ := json.Marshal(parseWaitingFor([]byte(strings.Join(c.lines, "\n"))))
		if string(got) != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestReadWaitingFor(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	call := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`
	// Padding larger than the tail pushes the start of the file out of the window,
	// and the line cut by the window must not break the result.
	pad := strings.Repeat(`{"type":"attachment","x":"`+strings.Repeat("a", 1000)+`"}`+"\n", 300)
	big := write("big.jsonl", pad+call+"\n")
	if got, _ := json.Marshal(readWaitingFor(big)); string(got) != `{"kind":"permission","tool":"Bash","input":"ls"}` {
		t.Errorf("big: got %s", got)
	}
	if got := readWaitingFor(filepath.Join(dir, "missing.jsonl")); got != nil {
		t.Errorf("missing file: got %v, want nil", got)
	}
}

func TestParseTreeCaller(t *testing.T) {
	const surfaces = `"windows":[{"ref":"window:1","workspaces":[{"ref":"workspace:1","panes":[{"surfaces":[{"ref":"surface:1","type":"terminal"}]}]}]}]`
	for _, c := range []struct {
		name string
		in   string
		want callerInfo
	}{
		{"caller", `{"caller":{"surface_ref":"surface:9","window_ref":"window:2"},` + surfaces + `}`, callerInfo{"surface:9", "window:2"}},
		{"no caller", `{` + surfaces + `}`, callerInfo{}},
		{"null caller", `{"caller":null,` + surfaces + `}`, callerInfo{}},
	} {
		_, got, err := parseTree([]byte(c.in))
		if err != nil || got != c.want {
			t.Errorf("%s: got %v, %v; want %v", c.name, got, err, c.want)
		}
	}
}

func TestExcludeSelf(t *testing.T) {
	caller := callerInfo{Surface: "s3", Window: "window:1"}
	for _, c := range []struct {
		name string
		env  string
		want string
	}{
		{"inside cmux", "C3C4B075-780E-4D9E-88AB-62B2B2FC8158", "s1,s2,s4,s5,s6,s7,s8"},
		{"outside cmux", "", "s1,s2,s3,s4,s5,s6,s7,s8"},
	} {
		got := surfaces(excludeSurface(testRows(), selfSurface(c.env, caller)))
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	// A caller that cmux did not report leaves every row.
	if got := surfaces(excludeSurface(testRows(), selfSurface("x", callerInfo{}))); got != "s1,s2,s3,s4,s5,s6,s7,s8" {
		t.Errorf("no caller: got %q", got)
	}
}

func TestFilterWorkspace(t *testing.T) {
	const uuid = "020161D6-DE4F-4BE5-B881-59C777FF6AD8"
	mk := func(surface, ws, id string) *Row {
		return &Row{Surface: &surface, Workspace: &ws, wsID: id}
	}
	rows := func() []*Row {
		return []*Row{
			mk("s1", "workspace:1", uuid),
			mk("s2", "workspace:5", "AAAA"),
			mk("s3", "workspace:5", "AAAA"),
			mk("s4", "workspace:17", "BBBB"),
			mk("s5", "workspace:18", ""),
		}
	}
	for _, c := range []struct{ name, ws, want string }{
		{"ref", "workspace:5", "s2,s3"},
		{"number is the ref number", "5", "s2,s3"},
		{"number 1 is workspace:1, not index 1", "1", "s1"},
		{"number in another window", "17", "s4"},
		{"leading zero", "05", "s2,s3"},
		{"uuid", uuid, "s1"},
		{"uuid lower case", strings.ToLower(uuid), "s1"},
		{"unknown ref", "workspace:999", ""},
		{"unknown number", "99", ""},
		{"unknown uuid", "FFFFFFFF-0000-0000-0000-000000000000", ""},
		{"negative", "-1", ""},
		{"empty keeps all", "", "s1,s2,s3,s4,s5"},
	} {
		if got := surfaces(filterWorkspace(rows(), c.ws)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseTreeWorkspace(t *testing.T) {
	in := `{"windows":[{"ref":"window:1","workspaces":[{"ref":"workspace:5","id":"AAAA","index":1,"panes":[{"surfaces":[{"ref":"surface:1","type":"terminal"}]}]},
		{"ref":"workspace:6","panes":[{"surfaces":[{"ref":"surface:2","type":"terminal"}]}]}]}]}`
	rows, _, err := parseTree([]byte(in))
	if err != nil || len(rows) != 2 {
		t.Fatalf("got %d rows, %v", len(rows), err)
	}
	if rows[0].wsID != "AAAA" || rows[1].wsID != "" {
		t.Errorf("got %q and %q", rows[0].wsID, rows[1].wsID)
	}
}
