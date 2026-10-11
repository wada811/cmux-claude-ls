// Shell completion: the script printed by --completion.

package main

import (
	"flag"
	"fmt"
	"strings"
)

// completionShells are the values --completion accepts.
var completionShells = []string{"zsh"}

// flagArgs describes the value of a flag for the completion: the name shown
// as the hint and the zsh action that completes it ("" for a free-form value).
// Value lists come from the same definitions the flags are validated against.
var flagArgs = map[string]struct{ hint, action string }{
	"state":      {"state", commaList("state", stateNames())},
	"fields":     {"field", commaList("field", fieldNames())},
	"sort":       {"order", "(" + strings.Join(sortOrders, " ") + ")"},
	"completion": {"shell", "(" + strings.Join(completionShells, " ") + ")"},
	"older-than": {"duration", " "},
	"workspace":  {"workspace", " "},
}

// commaList is the zsh action for a comma-separated list of values.
func commaList(name string, values []string) string {
	return "{_values -s , " + name + " " + strings.Join(values, " ") + "}"
}

// zshQuote quotes s for the single-quoted words of the script, and escapes
// the brackets of an _arguments description.
func zshQuote(s string) string {
	s = strings.NewReplacer("[", `\[`, "]", `\]`).Replace(s)
	return strings.ReplaceAll(s, "'", `'\''`)
}

// completionScript returns the completion script for shell. It is built from
// the flag definitions, so a new flag shows up without editing it.
func completionScript(shell string) (string, error) {
	if shell != "zsh" {
		return "", fmt.Errorf("invalid --completion value %q (use %s)", shell, strings.Join(completionShells, ", "))
	}
	const name = "cmux-claude-surfaces"
	fs, _ := newFlagSet()
	var specs []string
	fs.VisitAll(func(f *flag.Flag) {
		if f.Name == "version" {
			specs = append(specs, fmt.Sprintf("'(- *)--version[%s]'", zshQuote(f.Usage)))
			return
		}
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			specs = append(specs, fmt.Sprintf("'--%s[%s]'", f.Name, zshQuote(f.Usage)))
			return
		}
		a := flagArgs[f.Name]
		specs = append(specs, fmt.Sprintf("'--%s=[%s]:%s:%s'", f.Name, zshQuote(f.Usage), zshQuote(a.hint), zshQuote(a.action)))
	})
	specs = append(specs, `'(- *)'{-h,--help}'[print the help]'`)

	var sb strings.Builder
	fmt.Fprintf(&sb, "#compdef %s\n\n", name)
	fmt.Fprintf(&sb, "_%s() {\n  _arguments -S \\\n", name)
	for i, s := range specs {
		sb.WriteString("    " + s)
		if i < len(specs)-1 {
			sb.WriteString(" \\")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("}\n\n")
	fmt.Fprintf(&sb, "if [ \"$funcstack[1]\" = \"_%s\" ]; then\n  _%s \"$@\"\nelse\n  compdef _%s %s\nfi\n", name, name, name, name)
	return sb.String(), nil
}
