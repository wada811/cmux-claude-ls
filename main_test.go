package main

import (
	"bytes"
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
	const tail = "\nPlease report it at https://github.com/wada811/cmux-claude-ls/issues"
	for _, c := range []struct{ ver, want string }{
		{"0.64.22 (102) [abc]", "cmux-claude-ls: x (cmux 0.64.22 (102) [abc] ; the output format may have changed)" + tail},
		{"", "cmux-claude-ls: x (the output format may have changed)" + tail},
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
		{"version", []string{"--version"}, 0, "cmux-claude-ls ", ""},
		{"unknown flag", []string{"--bogus"}, 2, "", "flag provided but not defined: -bogus"},
		{"positional", []string{"foo"}, 2, "", "unexpected argument: foo"},
	} {
		var out, errb bytes.Buffer
		code := run(c.args, &out, &errb)
		if code != c.wantCode || !strings.Contains(out.String(), c.wantOut) || !strings.Contains(errb.String(), c.err) {
			t.Errorf("%s: code=%d out=%q err=%q", c.name, code, out.String(), errb.String())
		}
		if c.wantCode == 2 && !strings.Contains(errb.String(), "usage: cmux-claude-ls") {
			t.Errorf("%s: no usage on stderr: %q", c.name, errb.String())
		}
	}
}
