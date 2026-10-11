package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionGolden(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--completion", "zsh"}, &out, &errb); code != 0 || errb.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, errb.String())
	}
	golden(t, "completion_zsh", out.String())
}

// The value lists come from the definitions the flags are validated against.
func TestCompletionValues(t *testing.T) {
	script, err := completionScript("zsh")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--state=[", strings.Join(stateNames(), " "),
		"--fields=[", strings.Join(fieldNames(), " "),
		"--sort=[", "(" + strings.Join(sortOrders, " ") + ")",
		"--completion=[", "(" + strings.Join(completionShells, " ") + ")",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	fs, _ := newFlagSet()
	fs.VisitAll(func(f *flag.Flag) {
		if !strings.Contains(script, "--"+f.Name) {
			t.Errorf("script lacks --%s", f.Name)
		}
	})
}

func TestCompletionUnknownShell(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--completion", "fish"}, &out, &errb); code != 2 || out.Len() != 0 ||
		!strings.Contains(errb.String(), `invalid --completion value "fish" (use zsh)`) {
		t.Errorf("code=%d stdout=%q stderr=%q", code, out.String(), errb.String())
	}
}

// zsh -n only parses, so it needs no completion system.
func TestCompletionZshSyntax(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not found")
	}
	script, _ := completionScript("zsh")
	path := filepath.Join(t.TempDir(), "_cmux-list-claude-sessions")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(zsh, "-n", path).CombinedOutput(); err != nil {
		t.Errorf("zsh -n: %v\n%s", err, out)
	}
}
