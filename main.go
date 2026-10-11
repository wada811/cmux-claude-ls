// Command cmux-list-claude-sessions lists cmux surfaces that run (or ran) Claude Code.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"
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

var home, _ = os.UserHomeDir()

// now returns the current time. Tests replace it.
var now = time.Now

// sortOrders are the values --sort accepts.
var sortOrders = []string{"tree", "state", "age"}

// options holds the values of the flags.
type options struct {
	json        bool
	version     bool
	state       string
	olderThan   string
	workspace   string
	excludeSelf bool
	sort        string
	fields      string
	completion  string
}

// newFlagSet defines the flags. The usage strings are the descriptions the
// shell completion shows; the help text has the full explanation.
func newFlagSet() (*flag.FlagSet, *options) {
	o := &options{}
	fs := flag.NewFlagSet("cmux-list-claude-sessions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&o.json, "json", false, "print the list as JSON")
	fs.BoolVar(&o.version, "version", false, "print the version and exit")
	fs.StringVar(&o.state, "state", "", "keep only these states (comma-separated)")
	fs.StringVar(&o.olderThan, "older-than", "", "keep only tabs whose last message is older than this")
	fs.StringVar(&o.workspace, "workspace", "", "keep only this workspace (ref, UUID or index)")
	fs.BoolVar(&o.excludeSelf, "exclude-self", false, "leave out the tab this command runs in")
	fs.StringVar(&o.sort, "sort", "tree", "sort order: "+orList(sortOrders))
	fs.StringVar(&o.fields, "fields", "", "fields to print (comma-separated)")
	fs.StringVar(&o.completion, "completion", "", "print the completion script for this shell ("+strings.Join(completionShells, ", ")+")")
	return fs, o
}

// orList joins items as "a, b or c".
func orList(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}

func run(args []string, stdout, stderr io.Writer) int {
	fs, o := newFlagSet()
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
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if o.version {
		fmt.Fprintf(stdout, "cmux-list-claude-sessions %s\n", versionString())
		return 0
	}
	if set["completion"] {
		script, err := completionScript(o.completion)
		if err != nil {
			return usageError(stderr, err.Error())
		}
		io.WriteString(stdout, script)
		return 0
	}

	var states map[State]bool
	var olderThan *time.Duration
	if set["state"] {
		var err error
		if states, err = parseStates(o.state); err != nil {
			return usageError(stderr, err.Error())
		}
	}
	if set["older-than"] {
		d, err := parseAge(o.olderThan)
		if err != nil {
			return usageError(stderr, err.Error())
		}
		olderThan = &d
	}
	if set["workspace"] && strings.TrimSpace(o.workspace) == "" {
		return usageError(stderr, "invalid --workspace value (use workspace:N, a UUID or an index)")
	}
	if !slices.Contains(sortOrders, o.sort) {
		return usageError(stderr, fmt.Sprintf("invalid --sort value %q (use %s)", o.sort, orList(sortOrders)))
	}
	cols := fields
	if !o.json {
		cols, _ = pickFields(strings.Join(tableFields, ","))
	}
	if set["fields"] {
		var err error
		if cols, err = pickFields(o.fields); err != nil {
			return usageError(stderr, err.Error())
		}
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
	if set["workspace"] {
		rows = filterWorkspace(rows, strings.TrimSpace(o.workspace), caller.WindowRef)
	}
	if o.excludeSelf {
		rows = excludeSurface(rows, selfSurface(os.Getenv("CMUX_SURFACE_ID"), caller))
	}
	rows = filterRows(rows, states, olderThan, now())
	sortRows(rows, o.sort)
	return emit(stdout, rows, o.json, cols)
}

func usageError(stderr io.Writer, msg string) int {
	fmt.Fprintf(stderr, "cmux-list-claude-sessions: %s\n%s\nRun 'cmux-list-claude-sessions --help' for details.\n", msg, usageLine)
	return 2
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
