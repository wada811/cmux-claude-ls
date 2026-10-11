package main

// State is the state of a Claude Code session in a tab, as shown in the
// state field. Claude Code reports the first four in its session file; a tab
// whose session no longer runs is stopped.
type State string

const (
	StateBusy    State = "busy"
	StateWaiting State = "waiting"
	StateIdle    State = "idle"
	StateShell   State = "shell"
	StateStopped State = "stopped"
	// StateUnknown is for a live session whose status Claude Code did not
	// report as a string.
	StateUnknown State = "unknown"
)

// Row is one listed surface. Its JSON and table forms come from fields; the
// exported names follow the JSON keys.
type Row struct {
	WindowRef      *string
	WorkspaceRef   *string
	WorkspaceTitle *string
	SurfaceRef     *string
	SurfaceTitle   *string
	State          State
	WaitingFor     WaitingFor // nil unless the session is waiting
	Updated        *int64
	Cwd            *string
	SessionID      string
	Pid            *int // nil when claude is not running
	TranscriptPath *string

	wsID    string // workspace UUID from `cmux tree`
	wsIndex int    // workspace index in its window, -1 when unknown
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
