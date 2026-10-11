package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
