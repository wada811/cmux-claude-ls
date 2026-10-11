// Command cmux-list-claude-sessions lists cmux surfaces that run (or ran) Claude Code.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

// version is set by GoReleaser via -ldflags.
var version = "dev"

// versionString falls back to the module version for `go install` builds,
// which do not get the ldflags.
func versionString() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return version
}

const tailBytes = 512 * 1024

// waitTailBytes is how much of a transcript's end is read to find what a
// waiting session is blocked on.
const waitTailBytes = 256 * 1024

// maxInputRunes caps the one-line summary of a permission request.
const maxInputRunes = 200

const usageLine = "usage: cmux-list-claude-sessions [--state LIST] [--older-than DURATION] [--exclude-self] [--sort tree|state|age] [--json] [--version]"

// helpText is written for both people and LLM agents: what the tool is for,
// what each value means and what to do next.
const helpText = `cmux-list-claude-sessions - list the Claude Code sessions in cmux tabs and find which one needs you.

When many Claude Code sessions run in cmux tabs, this lists every tab that
runs (or ran) claude, across all windows and workspaces, with its state.
Use it to:
  - find sessions waiting for your answer        (--state waiting)
  - find finished tabs that can be closed        (--state stopped --older-than 14d)
  - get a tab's session id and transcript path   (--json)

` + usageLine + `

Options:
  --state LIST           keep only these states, comma-separated, from busy,
                         waiting, idle, shell, stopped (e.g. waiting,idle)
  --older-than DURATION  keep only tabs whose last message is older than
                         DURATION: 30m, 12h, 14d or any Go duration (1h30m).
                         Tabs with no known last message are left out.
  --exclude-self         leave out the tab this command runs in (found through
                         cmux's CMUX_SURFACE_ID; does nothing outside cmux)
  --sort ORDER           tree   cmux sidebar order (default)
                         state  waiting, idle, shell, busy, stopped; newest
                                first within a state
                         age    newest last message first
  --json                 print the list as JSON (keys: window, workspace,
                         workspace_title, surface, surface_title, state,
                         waiting_for, updated, cwd, session_id, pid,
                         transcript_path)
  --version              print the version and exit
  -h, --help             print this help

Filters and --sort apply to the table and to --json alike. The JSON keys
session_id and transcript_path are named like the input of Claude Code hooks.

waiting_for says what a "waiting" session is blocked on, read from the end of
its transcript. It is null for other states and when the transcript cannot be
read. It is not shown in the table.
  {"kind": "question", "questions": [{"question": "...", "header": "...",
     "options": ["label", ...], "multi_select": false}]}
                         an AskUserQuestion is open
  {"kind": "permission", "tool": "Bash", "input": "<command or path>"}
                         a tool call awaits your approval; input is the
                         command (Bash), the file path (Edit, Write, Read) or
                         the tool input as one short line of JSON
  {"kind": "unknown"}    waiting, but no open question or tool call was found

Columns:
  WS#, SURFACE#  cmux refs: pass them as workspace:N / surface:N to cmux
  CWD            working directory of the session
  AGE            time since the last message in the session
  STATE          busy     working on a reply
                 waiting  blocked on a question or a permission prompt
                 idle     reply finished; at the prompt
                 shell    reply finished; background shells or agents still run
                 stopped  claude has exited; the tab is still open

Examples:
  cmux-list-claude-sessions
  cmux-list-claude-sessions --state waiting
  cmux-list-claude-sessions --state stopped --older-than 14d
  cmux-list-claude-sessions --sort state
  cmux-list-claude-sessions --exclude-self --sort state
  cmux-list-claude-sessions --json --state waiting | jq -r '.[].transcript_path'
  cmux-list-claude-sessions --state waiting --json | jq '.[].waiting_for'
  cmux read-screen --workspace workspace:2 --surface surface:9   # look at a listed tab

Exit status (like grep):
  0  at least one row was printed
  1  no row to print: no Claude tab, or the filters left none ("[]" with --json)
  2  error: bad usage, or cmux or its output could not be read

Tabs are matched to sessions by cmux's restore record (checkpoint_id), not by
tty, which outlives a closed tab and can be reused by another one.`

const issuesURL = "https://github.com/wada811/cmux-list-claude-sessions/issues"

var home, _ = os.UserHomeDir()

// Row is one listed surface. Field order is the JSON key order.
type Row struct {
	Window         *string `json:"window"`
	Workspace      *string `json:"workspace"`
	WorkspaceTitle *string `json:"workspace_title"`
	Surface        *string `json:"surface"`
	SurfaceTitle   *string `json:"surface_title"`
	State          any     `json:"state"`
	WaitingFor     any     `json:"waiting_for"`
	Updated        *int64  `json:"updated"`
	Cwd            *string `json:"cwd"`
	SessionID      string  `json:"session_id"`
	Pid            any     `json:"pid"`
	TranscriptPath *string `json:"transcript_path"`
}

type tree struct {
	// Caller is the surface cmux resolved from the environment of this process.
	Caller *struct {
		SurfaceRef *string `json:"surface_ref"`
		WindowRef  *string `json:"window_ref"`
	} `json:"caller"`
	Windows *[]struct {
		Ref        *string `json:"ref"`
		Workspaces []struct {
			Ref   *string `json:"ref"`
			Title *string `json:"title"`
			Panes []struct {
				Surfaces []struct {
					Type  *string `json:"type"`
					Ref   *string `json:"ref"`
					Title *string `json:"title"`
				} `json:"surfaces"`
			} `json:"panes"`
		} `json:"workspaces"`
	} `json:"windows"`
}

type restoreRecord struct {
	Kind          string `json:"kind"`
	CheckpointID  string `json:"checkpoint_id"`
	LaunchCommand *struct {
		WorkingDirectory *string `json:"working_directory"`
	} `json:"launch_command"`
}

// cmuxOutput runs `cmux <args> --json` and returns its stdout.
func cmuxOutput(args ...string) ([]byte, error) {
	return exec.Command("cmux", append(args, "--json")...).Output()
}

// cmuxVersion returns the first line of `cmux --version` without the leading
// "cmux ", or "" when it cannot be read.
func cmuxVersion() string {
	out, err := exec.Command("cmux", "--version").Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(strings.TrimPrefix(line, "cmux "))
}

// formatError builds the message for an output format that could not be read.
func formatError(what, cmuxVer string) string {
	detail := "the output format may have changed"
	if cmuxVer != "" {
		detail = "cmux " + cmuxVer + " ; " + detail
	}
	return fmt.Sprintf("cmux-list-claude-sessions: %s (%s)\nPlease report it at %s", what, detail, issuesURL)
}

// formatErr is an error whose message should get the cmux version and the
// issue link attached.
type formatErr struct{ what string }

func (e *formatErr) Error() string { return e.what }

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// callerInfo is the surface that runs this process, as resolved by cmux.
// Fields are empty when cmux did not report them.
type callerInfo struct {
	Surface string
	Window  string
}

// parseTree extracts the terminal surfaces from `cmux tree --all --json`, and
// the caller cmux reports. It fails when the output has no `windows` key or no
// terminal surface.
func parseTree(data []byte) ([]*Row, callerInfo, error) {
	var t tree
	var caller callerInfo
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, caller, &formatErr{"could not parse the output of `cmux tree`"}
	}
	if t.Windows == nil {
		return nil, caller, &formatErr{"`cmux tree` output has no `windows`"}
	}
	if t.Caller != nil {
		caller = callerInfo{deref(t.Caller.SurfaceRef), deref(t.Caller.WindowRef)}
	}
	var rows []*Row
	for _, win := range *t.Windows {
		for _, ws := range win.Workspaces {
			for _, pane := range ws.Panes {
				for _, s := range pane.Surfaces {
					if s.Type != nil && *s.Type == "terminal" {
						rows = append(rows, &Row{
							Window: win.Ref, Workspace: ws.Ref, WorkspaceTitle: ws.Title,
							Surface: s.Ref, SurfaceTitle: s.Title,
						})
					}
				}
			}
		}
	}
	if len(rows) == 0 {
		return nil, caller, &formatErr{"`cmux tree` output has no terminal surface"}
	}
	return rows, caller, nil
}

// selfSurface returns the ref of the surface to leave out for --exclude-self.
// cmux reports its caller from the CMUX_SURFACE_ID of the process, which the
// surfaces of `cmux tree` do not carry as an id. Outside cmux (envSurfaceID
// empty) there is no self, and nothing is left out.
func selfSurface(envSurfaceID string, caller callerInfo) string {
	if envSurfaceID == "" {
		return ""
	}
	return caller.Surface
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

func listSurfaces() ([]*Row, callerInfo, error) {
	out, err := cmuxOutput("tree", "--all")
	if err != nil {
		return nil, callerInfo{}, errors.New("cmux-list-claude-sessions: could not read `cmux tree` (is cmux running?)")
	}
	return parseTree(out)
}

// parseRestoreRecord interprets the output of `cmux surface resume show`.
// readable is false when the output cannot be trusted: the command failed, the
// JSON is broken, `restore_record` is missing, or a claude record has no
// checkpoint_id. rec is non-nil only for a usable Claude record; a null record
// or another kind is readable and simply not Claude.
func parseRestoreRecord(out []byte, runErr error) (rec *restoreRecord, readable bool) {
	if runErr != nil {
		return nil, false
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(out, &top) != nil {
		return nil, false
	}
	raw, ok := top["restore_record"]
	if !ok {
		return nil, false
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, true
	}
	var r restoreRecord
	if json.Unmarshal(raw, &r) != nil {
		return nil, false
	}
	if r.Kind != "claude" {
		return nil, true
	}
	if r.CheckpointID == "" {
		return nil, false
	}
	return &r, true
}

func getRestoreRecord(row *Row) (*restoreRecord, bool) {
	out, err := cmuxOutput("surface", "resume", "show", "--window", deref(row.Window),
		"--workspace", deref(row.Workspace), "--surface", deref(row.Surface))
	return parseRestoreRecord(out, err)
}

type recordVerdict int

const (
	recordsOK   recordVerdict = iota
	recordsWarn               // some could not be read
	recordsFail               // none could be read
)

// judgeRecords decides what to do given how many of total surfaces had an
// unreadable resume record.
func judgeRecords(total, unreadable int) recordVerdict {
	switch {
	case unreadable == 0:
		return recordsOK
	case unreadable >= total:
		return recordsFail
	}
	return recordsWarn
}

type sessionMeta struct {
	raw       map[string]any
	updatedAt float64
}

// parseSessionFile reads a ~/.claude/sessions/<pid>.json. ok is false unless
// both pid and sessionId could be read.
func parseSessionFile(data []byte) (meta map[string]any, pid int, sid string, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if dec.Decode(&meta) != nil || meta == nil {
		return nil, 0, "", false
	}
	pid, pidOK := toInt(meta["pid"])
	sid, _ = meta["sessionId"].(string)
	if !pidOK || sid == "" {
		return nil, 0, "", false
	}
	return meta, pid, sid, true
}

// sessionsUnreadable reports whether session files exist but none of them had
// a pid and sessionId, which means the file format probably changed.
func sessionsUnreadable(total, readable int) bool {
	return total > 0 && readable == 0
}

// liveSessions maps sessionId -> ~/.claude/sessions/<pid>.json (live processes
// only). total and readable count the files found and the ones whose pid and
// sessionId could be read (a dead pid still counts as readable).
func liveSessions() (bySid map[string]*sessionMeta, total, readable int) {
	bySid = map[string]*sessionMeta{}
	paths, _ := filepath.Glob(filepath.Join(home, ".claude", "sessions", "*.json"))
	total = len(paths)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		meta, pid, sid, ok := parseSessionFile(data)
		if !ok {
			continue
		}
		readable++
		if syscall.Kill(pid, 0) != nil {
			continue
		}
		var upd float64
		if n, ok := meta["updatedAt"].(json.Number); ok {
			upd, _ = n.Float64()
		}
		if prev, ok := bySid[sid]; !ok || upd >= prev.updatedAt {
			bySid[sid] = &sessionMeta{raw: meta, updatedAt: upd}
		}
	}
	return bySid, total, readable
}

func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return int(i), true
		}
		f, err := x.Float64()
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return int(f), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(x))
		return i, err == nil
	}
	return 0, false
}

// lastTimestamp returns the last timestamp in a JSONL. The file mtime is used
// only as a fallback: it can be bumped in bulk without the content changing.
func lastTimestamp(path string) int64 {
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		if st, err := f.Stat(); err == nil {
			off := st.Size() - tailBytes
			if off < 0 {
				off = 0
			}
			if _, err := f.Seek(off, io.SeekStart); err == nil {
				data, _ := io.ReadAll(f)
				lines := strings.Split(string(data), "\n")
				for i := len(lines) - 1; i >= 0; i-- {
					var obj struct {
						Timestamp any `json:"timestamp"`
					}
					if json.Unmarshal([]byte(lines[i]), &obj) != nil {
						continue
					}
					ts, _ := obj.Timestamp.(string)
					if ts == "" {
						continue
					}
					if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
						return t.Unix()
					}
				}
			}
		}
	}
	if st, err := os.Stat(path); err == nil {
		return st.ModTime().Unix()
	}
	return 0
}

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

func describe(row *Row, rec *restoreRecord, live map[string]*sessionMeta) {
	sid := rec.CheckpointID
	m := live[sid]
	var jsonl *string
	paths, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", sid+".jsonl"))
	var best time.Time
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if jsonl == nil || st.ModTime().After(best) {
			p := p
			jsonl, best = &p, st.ModTime()
		}
	}
	row.SessionID = sid
	row.TranscriptPath = jsonl
	if jsonl != nil {
		ts := lastTimestamp(*jsonl)
		row.Updated = &ts
	}
	var cwd *string
	if m != nil {
		row.State = "unknown"
		if s, ok := m.raw["status"]; ok {
			row.State = s
		}
		row.Pid = m.raw["pid"]
		if s, ok := m.raw["cwd"].(string); ok && s != "" {
			cwd = &s
		}
	} else {
		row.State = "stopped"
	}
	if cwd == nil && rec.LaunchCommand != nil && rec.LaunchCommand.WorkingDirectory != nil &&
		*rec.LaunchCommand.WorkingDirectory != "" {
		cwd = rec.LaunchCommand.WorkingDirectory
	}
	row.Cwd = cwd
	if row.State == "waiting" && jsonl != nil {
		row.WaitingFor = readWaitingFor(*jsonl)
	}
}

// Ranges of code points whose East Asian Width is W or F (Unicode 16.0).
var wideRanges = [][2]rune{
	{0x1100, 0x115F},
	{0x231A, 0x231B},
	{0x2329, 0x232A},
	{0x23E9, 0x23EC},
	{0x23F0, 0x23F0},
	{0x23F3, 0x23F3},
	{0x25FD, 0x25FE},
	{0x2614, 0x2615},
	{0x2630, 0x2637},
	{0x2648, 0x2653},
	{0x267F, 0x267F},
	{0x268A, 0x268F},
	{0x2693, 0x2693},
	{0x26A1, 0x26A1},
	{0x26AA, 0x26AB},
	{0x26BD, 0x26BE},
	{0x26C4, 0x26C5},
	{0x26CE, 0x26CE},
	{0x26D4, 0x26D4},
	{0x26EA, 0x26EA},
	{0x26F2, 0x26F3},
	{0x26F5, 0x26F5},
	{0x26FA, 0x26FA},
	{0x26FD, 0x26FD},
	{0x2705, 0x2705},
	{0x270A, 0x270B},
	{0x2728, 0x2728},
	{0x274C, 0x274C},
	{0x274E, 0x274E},
	{0x2753, 0x2755},
	{0x2757, 0x2757},
	{0x2795, 0x2797},
	{0x27B0, 0x27B0},
	{0x27BF, 0x27BF},
	{0x2B1B, 0x2B1C},
	{0x2B50, 0x2B50},
	{0x2B55, 0x2B55},
	{0x2E80, 0x2E99},
	{0x2E9B, 0x2EF3},
	{0x2F00, 0x2FD5},
	{0x2FF0, 0x303E},
	{0x3041, 0x3096},
	{0x3099, 0x30FF},
	{0x3105, 0x312F},
	{0x3131, 0x318E},
	{0x3190, 0x31E5},
	{0x31EF, 0x321E},
	{0x3220, 0x3247},
	{0x3250, 0xA48C},
	{0xA490, 0xA4C6},
	{0xA960, 0xA97C},
	{0xAC00, 0xD7A3},
	{0xF900, 0xFAFF},
	{0xFE10, 0xFE19},
	{0xFE30, 0xFE52},
	{0xFE54, 0xFE66},
	{0xFE68, 0xFE6B},
	{0xFF01, 0xFF60},
	{0xFFE0, 0xFFE6},
	{0x16FE0, 0x16FE4},
	{0x16FF0, 0x16FF1},
	{0x17000, 0x187F7},
	{0x18800, 0x18CD5},
	{0x18CFF, 0x18D08},
	{0x1AFF0, 0x1AFF3},
	{0x1AFF5, 0x1AFFB},
	{0x1AFFD, 0x1AFFE},
	{0x1B000, 0x1B122},
	{0x1B132, 0x1B132},
	{0x1B150, 0x1B152},
	{0x1B155, 0x1B155},
	{0x1B164, 0x1B167},
	{0x1B170, 0x1B2FB},
	{0x1D300, 0x1D356},
	{0x1D360, 0x1D376},
	{0x1F004, 0x1F004},
	{0x1F0CF, 0x1F0CF},
	{0x1F18E, 0x1F18E},
	{0x1F191, 0x1F19A},
	{0x1F200, 0x1F202},
	{0x1F210, 0x1F23B},
	{0x1F240, 0x1F248},
	{0x1F250, 0x1F251},
	{0x1F260, 0x1F265},
	{0x1F300, 0x1F320},
	{0x1F32D, 0x1F335},
	{0x1F337, 0x1F37C},
	{0x1F37E, 0x1F393},
	{0x1F3A0, 0x1F3CA},
	{0x1F3CF, 0x1F3D3},
	{0x1F3E0, 0x1F3F0},
	{0x1F3F4, 0x1F3F4},
	{0x1F3F8, 0x1F43E},
	{0x1F440, 0x1F440},
	{0x1F442, 0x1F4FC},
	{0x1F4FF, 0x1F53D},
	{0x1F54B, 0x1F54E},
	{0x1F550, 0x1F567},
	{0x1F57A, 0x1F57A},
	{0x1F595, 0x1F596},
	{0x1F5A4, 0x1F5A4},
	{0x1F5FB, 0x1F64F},
	{0x1F680, 0x1F6C5},
	{0x1F6CC, 0x1F6CC},
	{0x1F6D0, 0x1F6D2},
	{0x1F6D5, 0x1F6D7},
	{0x1F6DC, 0x1F6DF},
	{0x1F6EB, 0x1F6EC},
	{0x1F6F4, 0x1F6FC},
	{0x1F7E0, 0x1F7EB},
	{0x1F7F0, 0x1F7F0},
	{0x1F90C, 0x1F93A},
	{0x1F93C, 0x1F945},
	{0x1F947, 0x1F9FF},
	{0x1FA70, 0x1FA7C},
	{0x1FA80, 0x1FA89},
	{0x1FA8F, 0x1FAC6},
	{0x1FACE, 0x1FADC},
	{0x1FADF, 0x1FAE9},
	{0x1FAF0, 0x1FAF8},
	{0x20000, 0x2FFFD},
	{0x30000, 0x3FFFD},
}

func runeWidth(r rune) int {
	if r < 0x1100 {
		return 1
	}
	i := sort.Search(len(wideRanges), func(i int) bool { return wideRanges[i][1] >= r })
	if i < len(wideRanges) && wideRanges[i][0] <= r {
		return 2
	}
	return 1
}

func width(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func isSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

func fit(s string, w int) string {
	s = strings.Join(strings.FieldsFunc(s, isSpace), " ")
	if width(s) > w {
		rs := []rune(s)
		for len(rs) > 0 && width(string(rs)) > w-1 {
			rs = rs[:len(rs)-1]
		}
		s = string(rs) + "…"
	}
	if pad := w - width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func ago(ts *int64, now time.Time) string {
	if ts == nil {
		return "-"
	}
	d := float64(now.UnixNano())/1e9 - float64(*ts)
	for _, u := range []struct {
		unit string
		sec  float64
	}{{"d", 86400}, {"h", 3600}, {"m", 60}} {
		if d >= u.sec {
			return strconv.Itoa(int(math.Floor(d/u.sec))) + u.unit
		}
	}
	return "now"
}

func num(ref *string) string {
	s := deref(ref)
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return s[i+1:]
	}
	return s
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

func printTable(w io.Writer, rows []*Row) {
	cols := []struct {
		name string
		w    int
	}{{"WS#", 4}, {"WORKSPACE", 18}, {"SURFACE#", 8}, {"SURFACE", 32}, {"CWD", 28}, {"STATE", 8}, {"AGE", 4}}
	var sb strings.Builder
	emit := func(vals []string) {
		parts := make([]string, len(cols))
		for i, c := range cols {
			parts[i] = fit(vals[i], c.w)
		}
		sb.WriteString(strings.TrimRight(strings.Join(parts, "  "), " \t\n\r\v\f"))
		sb.WriteByte('\n')
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.name
	}
	emit(names)
	now := time.Now()
	for _, r := range rows {
		cwd := strings.ReplaceAll(deref(r.Cwd), home, "~")
		emit([]string{num(r.Workspace), deref(r.WorkspaceTitle), num(r.Surface), deref(r.SurfaceTitle),
			cwd, fmt.Sprint(r.State), ago(r.Updated, now)})
	}
	io.WriteString(w, sb.String())
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cmux-list-claude-sessions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonMode := fs.Bool("json", false, "print the list as JSON")
	showVersion := fs.Bool("version", false, "print the version and exit")
	stateFlag := fs.String("state", "", "keep only these states (comma-separated)")
	olderFlag := fs.String("older-than", "", "keep only tabs whose last message is older than this")
	excludeSelf := fs.Bool("exclude-self", false, "leave out the tab this command runs in")
	sortFlag := fs.String("sort", "tree", "tree, state or age")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			io.WriteString(stdout, helpText+"\n")
			return 0
		}
		return usageError(stderr, err.Error())
	}
	if fs.NArg() > 0 {
		return usageError(stderr, "unexpected argument: "+strings.Join(fs.Args(), " "))
	}
	if *showVersion {
		fmt.Fprintf(stdout, "cmux-list-claude-sessions %s\n", versionString())
		return 0
	}

	var states map[string]bool
	var olderThan *time.Duration
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["state"] {
		var err error
		if states, err = parseStates(*stateFlag); err != nil {
			return usageError(stderr, err.Error())
		}
	}
	if set["older-than"] {
		d, err := parseAge(*olderFlag)
		if err != nil {
			return usageError(stderr, err.Error())
		}
		olderThan = &d
	}
	if *sortFlag != "tree" && *sortFlag != "state" && *sortFlag != "age" {
		return usageError(stderr, fmt.Sprintf("invalid --sort value %q (use tree, state or age)", *sortFlag))
	}

	fail := func(err error) int {
		var fe *formatErr
		if errors.As(err, &fe) {
			fmt.Fprintln(stderr, formatError(fe.what, cmuxVersion()))
		} else {
			fmt.Fprintln(stderr, err)
		}
		return 2
	}
	surfaces, caller, err := listSurfaces()
	if err != nil {
		return fail(err)
	}
	recs := make([]*restoreRecord, len(surfaces))
	bad := make([]bool, len(surfaces))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, s := range surfaces {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			rec, ok := getRestoreRecord(s)
			recs[i], bad[i] = rec, !ok
		}()
	}
	wg.Wait()
	unreadable := 0
	for _, b := range bad {
		if b {
			unreadable++
		}
	}
	switch judgeRecords(len(surfaces), unreadable) {
	case recordsFail:
		return fail(&formatErr{fmt.Sprintf(
			"could not read the restore record of any of the %d terminal surface(s) from `cmux surface resume show`",
			len(surfaces))})
	case recordsWarn:
		fmt.Fprintf(stderr, "cmux-list-claude-sessions: warning: could not read the restore record of %d surface(s)\n", unreadable)
	}
	live, total, readable := liveSessions()
	if sessionsUnreadable(total, readable) {
		fmt.Fprintf(stderr, "cmux-list-claude-sessions: warning: could not read pid and sessionId from any of the %d file(s) in ~/.claude/sessions; STATE may be wrong (the Claude Code file format may have changed)\n", total)
	}
	rows := []*Row{}
	for i, s := range surfaces {
		if recs[i] != nil {
			describe(s, recs[i], live)
			rows = append(rows, s)
		}
	}
	if *excludeSelf {
		rows = excludeSurface(rows, selfSurface(os.Getenv("CMUX_SURFACE_ID"), caller))
	}
	rows = filterRows(rows, states, olderThan, time.Now())
	sortRows(rows, *sortFlag)
	return emit(stdout, rows, *jsonMode)
}

// emit prints rows and returns the exit status: 0 if any row was printed,
// 1 if there was none.
func emit(stdout io.Writer, rows []*Row, jsonMode bool) int {
	if jsonMode {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", " ")
		enc.Encode(rows)
	} else {
		printTable(stdout, rows)
	}
	if len(rows) == 0 {
		return 1
	}
	return 0
}

func usageError(stderr io.Writer, msg string) int {
	fmt.Fprintf(stderr, "cmux-list-claude-sessions: %s\n%s\nRun 'cmux-list-claude-sessions --help' for details.\n", msg, usageLine)
	return 2
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
