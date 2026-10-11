// Command cmux-claude-ls lists cmux surfaces that run (or ran) Claude Code.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

var version = "dev"

const tailBytes = 512 * 1024

const helpText = `List cmux surfaces that run (or ran) Claude Code.

usage: cmux-claude-ls [--json] [--version]

A surface is listed when ` + "`cmux surface resume show`" + ` has a Claude restore record
for it. Tabs whose claude has exited are shown as "stopped"; plain shell tabs
are not shown. Surfaces are matched to sessions by the restore record's
checkpoint_id (the tty in ` + "`cmux tree`" + ` is not used: it outlives a closed tab and
can be reused by another one).`

var home, _ = os.UserHomeDir()

// Row is one listed surface. Field order is the JSON key order.
type Row struct {
	Window         *string `json:"window"`
	Workspace      *string `json:"workspace"`
	WorkspaceTitle *string `json:"workspace_title"`
	Surface        *string `json:"surface"`
	Title          *string `json:"title"`
	State          any     `json:"state"`
	Updated        *int64  `json:"updated"`
	Cwd            *string `json:"cwd"`
	Session        string  `json:"session"`
	Pid            any     `json:"pid"`
	Jsonl          *string `json:"jsonl"`
}

type tree struct {
	Windows []struct {
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

func cmuxJSON(v any, args ...string) bool {
	out, err := exec.Command("cmux", append(args, "--json")...).Output()
	if err != nil {
		return false
	}
	return json.Unmarshal(out, v) == nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func listSurfaces() []*Row {
	var t tree
	if !cmuxJSON(&t, "tree", "--all") {
		fmt.Fprintln(os.Stderr, "cmux-claude-ls: could not read `cmux tree` (is cmux running?)")
		os.Exit(1)
	}
	var rows []*Row
	for _, win := range t.Windows {
		for _, ws := range win.Workspaces {
			for _, pane := range ws.Panes {
				for _, s := range pane.Surfaces {
					if s.Type != nil && *s.Type == "terminal" {
						rows = append(rows, &Row{
							Window: win.Ref, Workspace: ws.Ref, WorkspaceTitle: ws.Title,
							Surface: s.Ref, Title: s.Title,
						})
					}
				}
			}
		}
	}
	return rows
}

func getRestoreRecord(row *Row) *restoreRecord {
	var r struct {
		RestoreRecord *restoreRecord `json:"restore_record"`
	}
	if !cmuxJSON(&r, "surface", "resume", "show", "--window", deref(row.Window),
		"--workspace", deref(row.Workspace), "--surface", deref(row.Surface)) {
		return nil
	}
	rec := r.RestoreRecord
	if rec != nil && rec.Kind == "claude" && rec.CheckpointID != "" {
		return rec
	}
	return nil
}

type sessionMeta struct {
	raw       map[string]any
	updatedAt float64
}

// liveSessions maps sessionId -> ~/.claude/sessions/<pid>.json (live processes only).
func liveSessions() map[string]*sessionMeta {
	bySid := map[string]*sessionMeta{}
	paths, _ := filepath.Glob(filepath.Join(home, ".claude", "sessions", "*.json"))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var meta map[string]any
		if dec.Decode(&meta) != nil {
			continue
		}
		pid, ok := toInt(meta["pid"])
		if !ok || syscall.Kill(pid, 0) != nil {
			continue
		}
		sid, _ := meta["sessionId"].(string)
		if sid == "" {
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
	return bySid
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
	row.Session = sid
	row.Jsonl = jsonl
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

func printTable(rows []*Row) {
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
		emit([]string{num(r.Workspace), deref(r.WorkspaceTitle), num(r.Surface), deref(r.Title),
			cwd, fmt.Sprint(r.State), ago(r.Updated, now)})
	}
	os.Stdout.WriteString(sb.String())
}

func main() {
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println(helpText)
		return
	}
	if len(args) > 0 && args[0] == "--version" {
		fmt.Printf("cmux-claude-ls %s\n", version)
		return
	}
	jsonMode := len(args) == 1 && args[0] == "--json"
	if len(args) > 0 && !jsonMode {
		fmt.Fprintf(os.Stderr, "cmux-claude-ls: unknown argument: %s\n", strings.Join(args, " "))
		os.Exit(1)
	}
	surfaces := listSurfaces()
	recs := make([]*restoreRecord, len(surfaces))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, s := range surfaces {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			recs[i] = getRestoreRecord(s)
		}()
	}
	wg.Wait()
	live := liveSessions()
	rows := []*Row{}
	for i, s := range surfaces {
		if recs[i] != nil {
			describe(s, recs[i], live)
			rows = append(rows, s)
		}
	}
	if jsonMode {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", " ")
		enc.Encode(rows)
	} else {
		printTable(rows)
	}
}
