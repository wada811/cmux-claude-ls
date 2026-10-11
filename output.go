// Output: field definitions, the table and JSON.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

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

// field is one value of a row, shown as a table column and a JSON key of the
// same name: text is the form for people, value the form for programs.
type field struct {
	name  string // JSON key; the table header is its upper case
	width int    // table column width
	text  func(r *Row, now time.Time) string
	value func(r *Row, now time.Time) any
}

func str(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// fields lists every field in the JSON key order.
var fields = []field{
	{"window_ref", 10, func(r *Row, _ time.Time) string { return deref(r.Window) },
		func(r *Row, _ time.Time) any { return str(r.Window) }},
	{"workspace_ref", 13, func(r *Row, _ time.Time) string { return deref(r.Workspace) },
		func(r *Row, _ time.Time) any { return str(r.Workspace) }},
	{"workspace_title", 18, func(r *Row, _ time.Time) string { return deref(r.WorkspaceTitle) },
		func(r *Row, _ time.Time) any { return str(r.WorkspaceTitle) }},
	{"surface_ref", 12, func(r *Row, _ time.Time) string { return deref(r.Surface) },
		func(r *Row, _ time.Time) any { return str(r.Surface) }},
	{"surface_title", 32, func(r *Row, _ time.Time) string { return deref(r.SurfaceTitle) },
		func(r *Row, _ time.Time) any { return str(r.SurfaceTitle) }},
	{"state", 8, func(r *Row, _ time.Time) string { return fmt.Sprint(r.State) },
		func(r *Row, _ time.Time) any { return r.State }},
	{"waiting_for", 32, func(r *Row, _ time.Time) string { return waitingText(r.WaitingFor) },
		func(r *Row, _ time.Time) any { return r.WaitingFor }},
	{"updated", 16, func(r *Row, _ time.Time) string {
		if r.Updated == nil {
			return "-"
		}
		return time.Unix(*r.Updated, 0).Local().Format("2006-01-02 15:04")
	}, func(r *Row, _ time.Time) any {
		if r.Updated == nil {
			return nil
		}
		return *r.Updated
	}},
	{"age", 4, func(r *Row, now time.Time) string { return ago(r.Updated, now) },
		func(r *Row, now time.Time) any {
			if r.Updated == nil {
				return nil
			}
			return max(now.Unix()-*r.Updated, 0)
		}},
	{"cwd", 28, func(r *Row, _ time.Time) string { return strings.ReplaceAll(deref(r.Cwd), home, "~") },
		func(r *Row, _ time.Time) any { return str(r.Cwd) }},
	{"session_id", 36, func(r *Row, _ time.Time) string { return r.SessionID },
		func(r *Row, _ time.Time) any { return r.SessionID }},
	{"pid", 7, func(r *Row, _ time.Time) string {
		if r.Pid == nil {
			return "-"
		}
		b, _ := json.Marshal(r.Pid)
		return string(b)
	}, func(r *Row, _ time.Time) any { return r.Pid }},
	{"transcript_path", 60, func(r *Row, _ time.Time) string { return strings.ReplaceAll(deref(r.TranscriptPath), home, "~") },
		func(r *Row, _ time.Time) any { return str(r.TranscriptPath) }},
}

// tableFields are the columns of the table when --fields is not given.
// JSON gets every field.
var tableFields = []string{"workspace_ref", "workspace_title", "surface_ref", "surface_title", "cwd", "state", "age"}

func fieldNames() []string {
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.name
	}
	return names
}

// pickFields turns "surface_ref,state" into fields, in the given order.
func pickFields(s string) ([]field, error) {
	var picked []field
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		i := slices.Index(fieldNames(), strings.ToLower(name))
		if i < 0 {
			return nil, fmt.Errorf("invalid --fields value %q (use %s)", name, strings.Join(fieldNames(), ", "))
		}
		picked = append(picked, fields[i])
	}
	return picked, nil
}

// object is a JSON object that keeps its key order.
type object struct {
	keys []string
	vals []any
}

func (o object) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := enc.Encode(k); err != nil {
			return nil, err
		}
		buf.WriteByte(':')
		if err := enc.Encode(o.vals[i]); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func printJSON(w io.Writer, rows []*Row, cols []field) {
	now := now()
	objs := make([]object, len(rows))
	for i, r := range rows {
		for _, f := range cols {
			objs[i].keys = append(objs[i].keys, f.name)
			objs[i].vals = append(objs[i].vals, f.value(r, now))
		}
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	enc.Encode(objs)
}

func printTable(w io.Writer, rows []*Row, cols []field) {
	var sb strings.Builder
	emit := func(vals []string) {
		parts := make([]string, len(cols))
		for i, c := range cols {
			parts[i] = fit(vals[i], c.width)
		}
		sb.WriteString(strings.TrimRight(strings.Join(parts, "  "), " \t\n\r\v\f"))
		sb.WriteByte('\n')
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = strings.ToUpper(c.name)
	}
	emit(names)
	now := now()
	for _, r := range rows {
		vals := make([]string, len(cols))
		for i, c := range cols {
			vals[i] = c.text(r, now)
		}
		emit(vals)
	}
	io.WriteString(w, sb.String())
}

// emit prints rows and returns the exit status: 0 if any row was printed,
// 1 if there was none.
func emit(stdout io.Writer, rows []*Row, jsonMode bool, cols []field) int {
	if jsonMode {
		printJSON(stdout, rows, cols)
	} else {
		printTable(stdout, rows, cols)
	}
	if len(rows) == 0 {
		return 1
	}
	return 0
}
