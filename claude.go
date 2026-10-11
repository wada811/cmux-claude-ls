// Claude Code side: ~/.claude sessions and transcripts.

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const tailBytes = 512 * 1024

// claudeDir is Claude Code's data directory (~/.claude). Tests replace it.
var claudeDir = filepath.Join(home, ".claude")

// pidAlive reports whether a process with this pid exists. Tests replace it.
var pidAlive = func(pid int) bool { return syscall.Kill(pid, 0) == nil }

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
	paths, _ := filepath.Glob(filepath.Join(claudeDir, "sessions", "*.json"))
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
		if !pidAlive(pid) {
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

func describe(row *Row, rec *restoreRecord, live map[string]*sessionMeta) {
	sid := rec.CheckpointID
	m := live[sid]
	var jsonl *string
	paths, _ := filepath.Glob(filepath.Join(claudeDir, "projects", "*", sid+".jsonl"))
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
