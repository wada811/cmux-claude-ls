package main

import (
	"testing"
	"time"
)

func TestWidth(t *testing.T) {
	for _, c := range []struct {
		s    string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"日本語", 6},
		{"あいうカナ", 10},
		{"한글", 4},
		{"ＡＢ", 4},
		{"a日b", 4},
		{"✳ x", 3},
		{"…", 1},
	} {
		if got := width(c.s); got != c.want {
			t.Errorf("width(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestFit(t *testing.T) {
	for _, c := range []struct {
		s    string
		w    int
		want string
	}{
		{"abc", 5, "abc  "},
		{"abcde", 5, "abcde"},
		{"abcdef", 5, "abcd…"},
		{"日本語", 8, "日本語  "},
		{"日本語テキスト", 8, "日本語… "}, // 6 cols + "…" = 7, padded to 8
		{"a  b\tc", 6, "a b c "},
		{"", 3, "   "},
	} {
		got := fit(c.s, c.w)
		if got != c.want {
			t.Errorf("fit(%q, %d) = %q, want %q", c.s, c.w, got, c.want)
		}
		if width(got) != c.w {
			t.Errorf("fit(%q, %d) width = %d", c.s, c.w, width(got))
		}
	}
}

func TestAgo(t *testing.T) {
	now := time.Unix(1_000_000_000, 0)
	at := func(sec int64) *int64 { v := now.Unix() - sec; return &v }
	for _, c := range []struct {
		ts   *int64
		want string
	}{
		{nil, "-"},
		{at(0), "now"},
		{at(59), "now"},
		{at(60), "1m"},
		{at(3599), "59m"},
		{at(3600), "1h"},
		{at(86399), "23h"},
		{at(86400), "1d"},
		{at(86400 * 5), "5d"},
		{at(-30), "now"},
	} {
		if got := ago(c.ts, now); got != c.want {
			t.Errorf("ago(%v) = %q, want %q", c.ts, got, c.want)
		}
	}
}
