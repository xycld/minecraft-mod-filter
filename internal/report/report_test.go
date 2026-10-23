package report

import "testing"

func TestWidth(t *testing.T) {
	for s, want := range map[string]int{"abc": 3, "仅客户端": 8, "服务端（客户端可选）": 20, "é": 1} {
		if got := width(s); got != want {
			t.Errorf("width(%q) = %d, want %d", s, got, want)
		}
	}
	if got := truncate("客户端客户端", 7); width(got) > 7 {
		t.Errorf("truncate overflow: %q", got)
	}
}

func TestCleanStripsControlSequences(t *testing.T) {
	in := "Evil\x1b]0;pwned\x07\x1b[2K‮Name\u0085"
	if got := clean(in); got != "Evil]0;pwned[2KName" {
		t.Errorf("clean = %q", got)
	}
	if got := mdEscape("a|b](x)\n"); got != `a\|b\](x)` {
		t.Errorf("mdEscape = %q", got)
	}
}
