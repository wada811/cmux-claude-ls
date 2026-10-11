// waiting_for: what a waiting session is blocked on, read from its transcript.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// waitTailBytes is how much of a transcript's end is read to find what a
// waiting session is blocked on.
const waitTailBytes = 256 * 1024

// maxInputRunes caps the one-line summary of a permission request.
const maxInputRunes = 200

type waitQuestion struct {
	Question    string   `json:"question"`
	Header      string   `json:"header"`
	Options     []string `json:"options"`
	MultiSelect bool     `json:"multi_select"`
}

type waitQuestions struct {
	Kind      string         `json:"kind"`
	Questions []waitQuestion `json:"questions"`
}

type waitPermission struct {
	Kind  string `json:"kind"`
	Tool  string `json:"tool"`
	Input string `json:"input"`
}

type waitUnknown struct {
	Kind string `json:"kind"`
}

// summarizeInput turns a tool_use input into one short line: the command for
// Bash, the path for file tools, the JSON otherwise.
func summarizeInput(tool string, input json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(input, &m) == nil {
		key := ""
		switch tool {
		case "Bash":
			key = "command"
		case "Edit", "Write", "Read", "MultiEdit":
			key = "file_path"
		case "NotebookEdit":
			key = "notebook_path"
		}
		if s, ok := m[key].(string); ok && key != "" {
			return oneLine(s)
		}
	}
	return oneLine(string(input))
}

func oneLine(s string) string {
	s = strings.Join(strings.FieldsFunc(s, isSpace), " ")
	if rs := []rune(s); len(rs) > maxInputRunes {
		s = string(rs[:maxInputRunes-1]) + "…"
	}
	return s
}

// parseWaitingFor finds the last tool_use in transcript lines that has no
// tool_result after it. Lines that are not JSON (such as a cut-off first
// line) are skipped. With no open tool_use it returns {"kind":"unknown"}.
func parseWaitingFor(data []byte) any {
	type block struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
	}
	type pendingCall struct {
		name  string
		input json.RawMessage
	}
	pending := map[string]pendingCall{}
	var order []string
	for _, line := range bytes.Split(data, []byte("\n")) {
		var e struct {
			Type        string `json:"type"`
			IsSidechain bool   `json:"isSidechain"`
			Message     struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil || e.IsSidechain || (e.Type != "assistant" && e.Type != "user") {
			continue
		}
		var blocks []block
		if json.Unmarshal(e.Message.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			switch b.Type {
			case "tool_use":
				pending[b.ID] = pendingCall{b.Name, b.Input}
				order = append(order, b.ID)
			case "tool_result":
				delete(pending, b.ToolUseID)
			}
		}
	}
	for i := len(order) - 1; i >= 0; i-- {
		call, ok := pending[order[i]]
		if !ok {
			continue
		}
		if call.name == "AskUserQuestion" {
			var in struct {
				Questions []struct {
					Question    string `json:"question"`
					Header      string `json:"header"`
					MultiSelect bool   `json:"multiSelect"`
					Options     []struct {
						Label string `json:"label"`
					} `json:"options"`
				} `json:"questions"`
			}
			if json.Unmarshal(call.input, &in) != nil || len(in.Questions) == 0 {
				return waitUnknown{"unknown"}
			}
			out := waitQuestions{Kind: "question", Questions: []waitQuestion{}}
			for _, q := range in.Questions {
				wq := waitQuestion{Question: q.Question, Header: q.Header, Options: []string{}, MultiSelect: q.MultiSelect}
				for _, o := range q.Options {
					wq.Options = append(wq.Options, o.Label)
				}
				out.Questions = append(out.Questions, wq)
			}
			return out
		}
		return waitPermission{"permission", call.name, summarizeInput(call.name, call.input)}
	}
	return waitUnknown{"unknown"}
}

// readWaitingFor reads the end of a transcript and extracts what the session
// waits for. It returns nil when the file cannot be read.
func readWaitingFor(path string) any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	off := st.Size() - waitTailBytes
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	if off > 0 { // the first line is probably cut off
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		} else {
			data = nil
		}
	}
	return parseWaitingFor(data)
}

// waitingText is the one-line table form of waiting_for.
func waitingText(w any) string {
	switch w := w.(type) {
	case waitQuestions:
		if len(w.Questions) == 0 {
			return "question"
		}
		q := w.Questions[0]
		t := q.Header
		if t == "" {
			t = q.Question
		}
		if n := len(w.Questions) - 1; n > 0 {
			t += fmt.Sprintf(" (+%d)", n)
		}
		return "question: " + t
	case waitPermission:
		return "permission: " + strings.TrimSpace(w.Tool+" "+w.Input)
	case waitUnknown:
		return "unknown"
	}
	return "-"
}
