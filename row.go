package main

// Row is one listed surface. Its JSON and table forms come from fields.
type Row struct {
	Window         *string
	Workspace      *string
	WorkspaceTitle *string
	Surface        *string
	SurfaceTitle   *string
	State          any
	WaitingFor     any
	Updated        *int64
	Cwd            *string
	SessionID      string
	Pid            any
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
