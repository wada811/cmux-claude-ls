package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the testdata/*.golden files")

// fixedNow is the clock of the end-to-end tests. The transcripts in testdata
// are timestamped relative to it.
var fixedNow = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func TestMain(m *testing.M) {
	// The table shows the last message in local time.
	time.Local = time.UTC
	os.Exit(m.Run())
}

// env describes the world a run sees: which cmux calls fail, where Claude
// Code's data lives, and which pids are alive.
type env struct {
	claude      string          // directory under testdata holding .claude
	treeFails   bool            // `cmux tree` fails
	treeRaw     string          // replaces the tree output when set
	resumeFails map[string]bool // surface ref -> `resume show` fails
	resumeRaw   map[string]string
	dead        map[int]bool // pids that are not alive
	cmuxVersion string       // output of `cmux --version`
	surfaceID   string       // CMUX_SURFACE_ID
}

func defaultEnv() env {
	return env{claude: "home", dead: map[int]bool{4099: true}, cmuxVersion: "cmux 9.9.9 (1)\n", surfaceID: "self"}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// install swaps the package variables for the fakes and restores them after
// the test. The fake cmux serves testdata/cmux/tree.json and
// testdata/cmux/resume_<N>.json.
func (e env) install(t *testing.T) {
	t.Helper()
	oldExec, oldHome, oldDir, oldNow, oldAlive := cmuxExec, home, claudeDir, now, pidAlive
	t.Cleanup(func() { cmuxExec, home, claudeDir, now, pidAlive = oldExec, oldHome, oldDir, oldNow, oldAlive })
	home = "/Users/me"
	claudeDir = filepath.Join("testdata", e.claude, ".claude")
	now = func() time.Time { return fixedNow }
	pidAlive = func(pid int) bool { return !e.dead[pid] }
	cmuxExec = func(args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "--version" {
			return []byte(e.cmuxVersion), nil
		}
		if len(args) == 0 || args[len(args)-1] != "--json" {
			return nil, fmt.Errorf("unexpected cmux call %v", args)
		}
		if args[0] == "tree" {
			if e.treeFails {
				return nil, errors.New("exit status 1")
			}
			if e.treeRaw != "" {
				return []byte(e.treeRaw), nil
			}
			return mustRead(t, "testdata/cmux/tree.json"), nil
		}
		if len(args) > 2 && args[0] == "surface" && args[1] == "resume" && args[2] == "show" {
			ref := ""
			for i, a := range args {
				if a == "--surface" {
					ref = args[i+1]
				}
			}
			if e.resumeFails[ref] {
				return nil, errors.New("exit status 1")
			}
			if raw, ok := e.resumeRaw[ref]; ok {
				return []byte(raw), nil
			}
			return mustRead(t, "testdata/cmux/resume_"+strings.TrimPrefix(ref, "surface:")+".json"), nil
		}
		return nil, fmt.Errorf("unexpected cmux call %v", args)
	}
	t.Setenv("CMUX_SURFACE_ID", e.surfaceID)
	// When a session id has a transcript in two project directories, the one
	// written last wins. Git does not keep mtimes, so set them here.
	for dir, age := range map[string]time.Duration{"-Users-me-work-beta": time.Hour, "-Users-me-work-other": 48 * time.Hour} {
		p := filepath.Join(claudeDir, "projects", dir, "00000000-0000-4000-8000-000000000010.jsonl")
		if _, err := os.Stat(p); err == nil {
			if err := os.Chtimes(p, fixedNow.Add(-age), fixedNow.Add(-age)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

type result struct {
	stdout, stderr string
	code           int
}

func (e env) run(t *testing.T, args ...string) result {
	t.Helper()
	e.install(t)
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return result{out.String(), errb.String(), code}
}

// golden compares got with testdata/<name>.golden; -update rewrites it.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if want := string(mustRead(t, path)); got != want {
		t.Errorf("%s differs (run `go test -update` to accept)\n--- got\n%s--- want\n%s", path, got, want)
	}
}

func TestRunGolden(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
	}{
		{"table", nil},
		{"table_sort_state", []string{"--sort", "state"}},
		{"table_sort_age", []string{"--sort", "age"}},
		{"table_fields", []string{"--fields", "surface_ref,state,age,waiting_for,pid"}},
		{"json", []string{"--json"}},
		{"json_waiting_fields", []string{"--json", "--state", "waiting", "--sort", "state", "--fields", "surface_ref,waiting_for"}},
		{"table_older_than", []string{"--older-than", "1d"}},
		{"table_workspace", []string{"--workspace", "workspace:2"}},
		{"table_exclude_self", []string{"--exclude-self"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := defaultEnv().run(t, c.args...)
			if r.code != 0 || r.stderr != "" {
				t.Fatalf("code %d, stderr %q", r.code, r.stderr)
			}
			golden(t, c.name, r.stdout)
		})
	}
}

func TestRunStates(t *testing.T) {
	r := defaultEnv().run(t, "--json", "--fields", "surface_ref,state,pid,cwd,session_id")
	want := map[string]string{
		"surface:1": "busy", "surface:2": "waiting", "surface:3": "waiting", "surface:4": "waiting",
		"surface:5": "idle", "surface:6": "shell", "surface:7": "stopped", "surface:10": "idle", "surface:11": "stopped",
	}
	for ref, state := range want {
		if !strings.Contains(r.stdout, `"surface_ref": "`+ref+`",`+"\n  "+`"state": "`+state+`"`) {
			t.Errorf("%s should be %s:\n%s", ref, state, r.stdout)
		}
	}
	for _, absent := range []string{"surface:8", "surface:9"} {
		if strings.Contains(r.stdout, absent) {
			t.Errorf("%s is not a Claude tab but was listed", absent)
		}
	}
	// A dead pid means stopped, with no pid and the restore record's cwd.
	if !strings.Contains(r.stdout, `"cwd": "/Users/me/work/restore-7"`) {
		t.Errorf("stopped tab should fall back to the restore record's cwd:\n%s", r.stdout)
	}
}

func TestRunWaitingFor(t *testing.T) {
	r := defaultEnv().run(t, "--json", "--state", "waiting", "--fields", "surface_ref,waiting_for")
	for _, want := range []string{
		`"kind": "question"`, `"header": "Layout"`, `"multi_select": true`, `"Nested"`,
		`"kind": "permission"`, `"tool": "Bash"`, `"input": "./migrate.sh --apply --force"`,
		`"kind": "unknown"`,
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("missing %s in\n%s", want, r.stdout)
		}
	}
}

func TestRunWarnings(t *testing.T) {
	t.Run("one restore record unreadable", func(t *testing.T) {
		e := defaultEnv()
		e.resumeFails = map[string]bool{"surface:11": true}
		r := e.run(t)
		if r.code != 0 {
			t.Errorf("code = %d, want 0", r.code)
		}
		if want := "cmux-claude-surfaces: warning: could not read the restore record of 1 surface(s)\n"; r.stderr != want {
			t.Errorf("stderr = %q", r.stderr)
		}
		if strings.Contains(r.stdout, "surface:11") || !strings.Contains(r.stdout, "surface:10") {
			t.Errorf("unreadable tab should be left out, others kept:\n%s", r.stdout)
		}
	})
	t.Run("broken restore record shapes", func(t *testing.T) {
		e := defaultEnv()
		e.resumeRaw = map[string]string{
			"surface:1": "not json",
			"surface:2": `{"cleared":false}`,
			"surface:3": `{"restore_record":{"kind":"claude"}}`,
		}
		r := e.run(t)
		if r.code != 0 || !strings.Contains(r.stderr, "restore record of 3 surface(s)") {
			t.Errorf("code %d, stderr %q", r.code, r.stderr)
		}
	})
	t.Run("no restore record readable", func(t *testing.T) {
		e := defaultEnv()
		e.resumeFails = map[string]bool{}
		for _, n := range []int{1, 2, 3, 4, 5, 6, 7, 8, 10, 11} {
			e.resumeFails[fmt.Sprintf("surface:%d", n)] = true
		}
		r := e.run(t)
		if r.code != 2 || r.stdout != "" {
			t.Errorf("code %d, stdout %q", r.code, r.stdout)
		}
		golden(t, "stderr_no_records", r.stderr)
	})
	t.Run("sessions unreadable", func(t *testing.T) {
		e := defaultEnv()
		e.claude = "home_badsessions"
		r := e.run(t, "--fields", "surface_ref,state")
		if r.code != 0 {
			t.Errorf("code = %d, want 0", r.code)
		}
		golden(t, "stderr_bad_sessions", r.stderr)
		// With no live session every Claude tab shows as stopped.
		if strings.Count(r.stdout, "stopped") != 9 {
			t.Errorf("want 9 stopped tabs:\n%s", r.stdout)
		}
	})
}

func TestRunCmuxFailures(t *testing.T) {
	t.Run("tree fails", func(t *testing.T) {
		e := defaultEnv()
		e.treeFails = true
		r := e.run(t)
		if r.code != 2 || r.stderr != "cmux-claude-surfaces: could not read `cmux tree` (is cmux running?)\n" {
			t.Errorf("code %d, stderr %q", r.code, r.stderr)
		}
	})
	for name, raw := range map[string]string{
		"tree is not json":      "oops",
		"tree has no windows":   `{"caller":{}}`,
		"tree has no terminals": `{"windows":[{"ref":"window:1","workspaces":[]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			e := defaultEnv()
			e.treeRaw = raw
			r := e.run(t)
			if r.code != 2 || r.stdout != "" {
				t.Errorf("code %d, stdout %q", r.code, r.stdout)
			}
			golden(t, "stderr_"+strings.ReplaceAll(name, " ", "_"), r.stderr)
		})
	}
	t.Run("format error without a cmux version", func(t *testing.T) {
		e := defaultEnv()
		e.treeRaw = "oops"
		e.cmuxVersion = ""
		r := e.run(t)
		if strings.Contains(r.stderr, "cmux ;") || !strings.Contains(r.stderr, "(the output format may have changed)") {
			t.Errorf("stderr = %q", r.stderr)
		}
	})
}

func TestRunFilters(t *testing.T) {
	refs := func(r result) []string {
		var out []string
		for _, l := range strings.Split(r.stdout, "\n")[1:] {
			if f := strings.Fields(l); len(f) > 0 {
				out = append(out, f[0])
			}
		}
		return out
	}
	for _, c := range []struct {
		name string
		args []string
		want string
		code int
	}{
		{"state list", []string{"--state", "waiting,busy", "--fields", "surface_ref"}, "surface:1 surface:2 surface:3 surface:4", 0},
		{"state none", []string{"--state", "shell", "--workspace", "workspace:2", "--fields", "surface_ref"}, "", 1},
		{"older than excludes unknown age", []string{"--older-than", "1d", "--fields", "surface_ref"}, "surface:7 surface:10", 0},
		{"workspace by uuid", []string{"--workspace", "22222222-2222-4222-8222-222222222222", "--fields", "surface_ref"}, "surface:10 surface:11", 0},
		{"workspace by index", []string{"--workspace", "1", "--fields", "surface_ref"}, "surface:10 surface:11", 0},
		{"unknown workspace", []string{"--workspace", "workspace:99", "--fields", "surface_ref"}, "", 1},
		{"exclude self", []string{"--exclude-self", "--state", "busy", "--fields", "surface_ref"}, "", 1},
		{"sort age", []string{"--sort", "age", "--fields", "surface_ref"}, "surface:1 surface:5 surface:3 surface:2 surface:4 surface:6 surface:7 surface:10 surface:11", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := defaultEnv().run(t, c.args...)
			if got := strings.Join(refs(r), " "); got != c.want || r.code != c.code {
				t.Errorf("rows %q code %d, want %q code %d\n%s", got, r.code, c.want, c.code, r.stdout)
			}
		})
	}
	t.Run("exclude self outside cmux does nothing", func(t *testing.T) {
		e := defaultEnv()
		e.surfaceID = ""
		r := e.run(t, "--exclude-self", "--state", "busy", "--fields", "surface_ref")
		if r.code != 0 || !strings.Contains(r.stdout, "surface:1") {
			t.Errorf("code %d\n%s", r.code, r.stdout)
		}
	})
	t.Run("json with no rows is an empty list", func(t *testing.T) {
		r := defaultEnv().run(t, "--json", "--state", "shell", "--workspace", "workspace:2")
		if r.code != 1 || r.stdout != "[]\n" {
			t.Errorf("code %d, stdout %q", r.code, r.stdout)
		}
	})
}

func TestRunFieldsAndSortErrors(t *testing.T) {
	for _, args := range [][]string{{"--fields", "nope"}, {"--sort", "nope"}, {"--state", "nope"}, {"--older-than", "x"}} {
		r := defaultEnv().run(t, args...)
		if r.code != 2 || r.stdout != "" || !strings.Contains(r.stderr, "Run 'cmux-claude-surfaces --help'") {
			t.Errorf("%v: code %d, stderr %q", args, r.code, r.stderr)
		}
	}
}
