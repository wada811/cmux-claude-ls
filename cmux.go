// cmux side: reading `cmux tree` and `cmux surface resume show`.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const issuesURL = "https://github.com/wada811/cmux-list-claude-sessions/issues"

// cmuxExec runs cmux with the given arguments and returns its stdout. Tests
// replace it.
var cmuxExec = func(args ...string) ([]byte, error) {
	return exec.Command("cmux", args...).Output()
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
			ID    *string `json:"id"`
			Index *int    `json:"index"`
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
	return cmuxExec(append(args, "--json")...)
}

// cmuxVersion returns the first line of `cmux --version` without the leading
// "cmux ", or "" when it cannot be read.
func cmuxVersion() string {
	out, err := cmuxExec("--version")
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
						idx := -1
						if ws.Index != nil {
							idx = *ws.Index
						}
						rows = append(rows, &Row{
							Window: win.Ref, Workspace: ws.Ref, WorkspaceTitle: ws.Title,
							Surface: s.Ref, SurfaceTitle: s.Title,
							wsID: deref(ws.ID), wsIndex: idx,
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
