package main

import (
	"errors"
	"strings"
	"testing"
)

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
	caller := callerInfo{SurfaceRef: "s3", WindowRef: "window:1"}
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
	mk := func(surface, window, ws string, idx int, id string) *Row {
		return &Row{SurfaceRef: &surface, WindowRef: &window, WorkspaceRef: &ws, wsIndex: idx, wsID: id}
	}
	rows := func() []*Row {
		return []*Row{
			mk("s1", "window:1", "workspace:1", 0, uuid),
			mk("s2", "window:1", "workspace:5", 1, "AAAA"),
			mk("s3", "window:1", "workspace:5", 1, "AAAA"),
			mk("s4", "window:3", "workspace:17", 0, "BBBB"),
			mk("s5", "window:3", "workspace:18", -1, ""),
		}
	}
	for _, c := range []struct {
		name         string
		ws           string
		callerWindow string
		want         string
	}{
		{"ref", "workspace:5", "window:1", "s2,s3"},
		{"ref in another window", "workspace:17", "window:1", "s4"},
		{"uuid", uuid, "window:1", "s1"},
		{"uuid lower case", strings.ToLower(uuid), "window:1", "s1"},
		{"index", "1", "window:1", "s2,s3"},
		{"index 0 in the caller's window", "0", "window:1", "s1"},
		{"index 0 in the other window", "0", "window:3", "s4"},
		{"index without caller window", "0", "", "s1,s4"},
		{"ref number is not an index", "workspace:2", "window:1", ""},
		{"unknown ref", "workspace:999", "window:1", ""},
		{"unknown index", "99", "window:1", ""},
		{"unknown uuid", "FFFFFFFF-0000-0000-0000-000000000000", "window:1", ""},
		{"unknown index never matches a row without index", "-1", "", ""},
		{"empty keeps all", "", "window:1", "s1,s2,s3,s4,s5"},
	} {
		if got := surfaces(filterWorkspace(rows(), c.ws, c.callerWindow)); got != c.want {
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
	if rows[0].wsID != "AAAA" || rows[0].wsIndex != 1 || rows[1].wsID != "" || rows[1].wsIndex != -1 {
		t.Errorf("got %q/%d and %q/%d", rows[0].wsID, rows[0].wsIndex, rows[1].wsID, rows[1].wsIndex)
	}
}
