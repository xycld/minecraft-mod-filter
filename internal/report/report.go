// Package report renders scan results as a terminal table, JSON or Markdown.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/xycld/minecraft-mod-filter/internal/detect"
	"github.com/xycld/minecraft-mod-filter/internal/i18n"
	"github.com/xycld/minecraft-mod-filter/internal/scan"
	"github.com/xycld/minecraft-mod-filter/internal/side"
)

type Options struct {
	Lang    i18n.Lang
	Explain bool // list every piece of evidence
	Color   bool
}

func JSON(w io.Writer, res *scan.Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

var categoryOrder = map[string]int{
	"client-only": 0, "client (server optional)": 1, "either": 2, "both": 3,
	"server (client optional)": 4, "server-only": 5,
}

func sorted(mods []*scan.Mod) []*scan.Mod {
	out := append([]*scan.Mod(nil), mods...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := order(out[i]), order(out[j])
		if a != b {
			return a < b
		}
		return strings.ToLower(out[i].Identity.Name) < strings.ToLower(out[j].Identity.Name)
	})
	return out
}

func order(m *scan.Mod) int {
	if o, ok := categoryOrder[m.Verdict.Category]; ok {
		return o
	}
	return len(categoryOrder)
}

// Reason renders one piece of evidence in the given language.
func Reason(l i18n.Lang, e detect.Evidence) string {
	args := make([]any, len(e.Args))
	for i, a := range e.Args {
		args[i] = a
	}
	return clean(l.T(e.Rule, args...))
}

// clean removes control characters and bidi overrides from text that comes
// from jars or the network, so it cannot drive the terminal.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r >= 0x7f && r < 0xa0, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			return -1
		}
		return r
	}, s)
}

// mainReason is the strongest evidence that agrees with the verdict, preferring
// the server side since that is the decision users act on most.
func mainReason(l i18n.Lang, v detect.Verdict) string {
	for _, agree := range []func(detect.Evidence) bool{
		func(e detect.Evidence) bool { return e.Server != side.Unknown && e.Server == v.Env.Server },
		func(e detect.Evidence) bool { return e.Client != side.Unknown && e.Client == v.Env.Client },
	} {
		for _, e := range v.Evidence {
			if agree(e) {
				return Reason(l, e)
			}
		}
	}
	if len(v.Evidence) > 0 {
		return Reason(l, v.Evidence[0])
	}
	return ""
}

func category(l i18n.Lang, c string) string { return l.T("cat." + c) }
func need(l i18n.Lang, n side.Need) string  { return l.T("need." + n.String()) }

const (
	reset  = "\x1b[0m"
	dim    = "\x1b[2m"
	bold   = "\x1b[1m"
	red    = "\x1b[31m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	blue   = "\x1b[34m"
)

func needColor(n side.Need) string {
	switch n {
	case side.Required:
		return green
	case side.Optional:
		return blue
	case side.Unsupported:
		return red
	}
	return yellow
}

// Table writes a human-readable table followed by an actionable summary.
func Table(w io.Writer, res *scan.Result, o Options) {
	l := o.Lang
	paint := func(code, s string) string {
		if !o.Color || code == "" {
			return s
		}
		return code + s + reset
	}
	header := []string{l.T("col.mod"), l.T("col.client"), l.T("col.server"), l.T("col.confidence"), l.T("col.reason")}
	rows := [][]string{}
	colors := [][]string{}
	mods := sorted(res.Mods)
	for _, m := range mods {
		v := m.Verdict
		reason := mainReason(l, v)
		confColor := ""
		if v.Confidence == detect.Low {
			confColor = yellow
		}
		rows = append(rows, []string{
			truncate(clean(m.Identity.Name), 40), need(l, v.Env.Client), need(l, v.Env.Server),
			l.T("conf." + string(v.Confidence)), truncate(reason, 70),
		})
		colors = append(colors, []string{"", needColor(v.Env.Client), needColor(v.Env.Server), confColor, dim})
	}
	widths := make([]int, len(header))
	for _, r := range append([][]string{header}, rows...) {
		for i, c := range r {
			widths[i] = max(widths[i], width(c))
		}
	}
	line := func(cells, cols []string) {
		var b strings.Builder
		for i, c := range cells {
			if i > 0 {
				b.WriteString("  ")
			}
			code := ""
			if cols != nil {
				code = cols[i]
			}
			pad := ""
			if i < len(cells)-1 {
				pad = strings.Repeat(" ", widths[i]-width(c))
			}
			b.WriteString(paint(code, c) + pad)
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
	line(header, []string{bold, bold, bold, bold, bold})
	for i, r := range rows {
		line(r, colors[i])
		if o.Explain {
			explain(w, l, mods[i], paint)
		}
	}
	fmt.Fprintln(w)
	summary(w, res, l, paint)
}

func explain(w io.Writer, l i18n.Lang, m *scan.Mod, paint func(string, string) string) {
	fmt.Fprintf(w, "    %s %s\n", paint(dim, l.T("col.file")+":"), clean(m.File))
	for _, e := range m.Verdict.Evidence {
		fmt.Fprintf(w, "    %s %-10s c=%-11s s=%-11s %s\n", paint(dim, fmt.Sprintf("[%3d]", e.Weight)), e.Source,
			e.Client, e.Server, Reason(l, e))
	}
	if len(m.RequiredBy) > 0 {
		fmt.Fprintf(w, "    %s %s\n", paint(dim, l.T("requiredBy")+":"), clean(strings.Join(m.RequiredBy, ", ")))
	}
	for _, p := range m.Problems {
		fmt.Fprintf(w, "    %s %s\n", paint(yellow, "!"), clean(p))
	}
}

func summary(w io.Writer, res *scan.Result, l i18n.Lang, paint func(string, string) string) {
	mode := l.T("summary.offline")
	if res.Online {
		mode = l.T("summary.online")
	}
	fmt.Fprintln(w, paint(bold, l.T("summary.title")))
	fmt.Fprintln(w, l.T("summary.total", len(res.Mods), mode))
	counts := map[string]int{}
	for _, m := range res.Mods {
		counts[m.Verdict.Category]++
	}
	var cats []string
	for c := range counts {
		cats = append(cats, c)
	}
	sort.Slice(cats, func(i, j int) bool { return categoryOrder[cats[i]] < categoryOrder[cats[j]] })
	for _, c := range cats {
		name := category(l, c)
		fmt.Fprintf(w, "  %s%s %d\n", name, strings.Repeat(" ", max(0, 28-width(name))), counts[c])
	}

	list := func(title string, keep func(*scan.Mod) bool) {
		var names []string
		for _, m := range sorted(res.Mods) {
			if keep(m) {
				names = append(names, m.File)
			}
		}
		if len(names) == 0 {
			return
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, paint(bold, fmt.Sprintf(l.T(title), len(names))))
		for _, n := range names {
			fmt.Fprintln(w, "  "+clean(n))
		}
	}
	list("summary.removeServer", func(m *scan.Mod) bool { return m.Verdict.Env.Server == side.Unsupported })
	list("summary.optionalServer", func(m *scan.Mod) bool { return m.Verdict.Env.Server == side.Optional })
	list("summary.removeClient", func(m *scan.Mod) bool { return m.Verdict.Env.Client == side.Unsupported })
	list("summary.lowConfidence", func(m *scan.Mod) bool { return m.Verdict.Confidence == detect.Low })

	if len(res.Missing) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, paint(yellow, l.T("summary.missing", clean(strings.Join(res.Missing, ", ")))))
	}
	if len(res.Warnings) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, paint(yellow, l.T("summary.warnings")))
		for _, s := range res.Warnings {
			fmt.Fprintln(w, "  "+clean(s))
		}
	}
}

// Markdown writes a Markdown table suitable for issues and pack READMEs.
func Markdown(w io.Writer, res *scan.Result, o Options) {
	l := o.Lang
	fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n|---|---|---|---|---|\n",
		l.T("col.mod"), l.T("col.client"), l.T("col.server"), l.T("col.confidence"), l.T("col.reason"))
	for _, m := range sorted(res.Mods) {
		v := m.Verdict
		reason := mainReason(l, v)
		name := mdEscape(m.Identity.Name)
		if m.Modrinth != nil && m.Modrinth.Slug != "" {
			name = fmt.Sprintf("[%s](https://modrinth.com/mod/%s)", name, url.PathEscape(m.Modrinth.Slug))
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n", name, need(l, v.Env.Client), need(l, v.Env.Server),
			l.T("conf."+string(v.Confidence)), mdEscape(reason))
	}
}

var mdReplacer = strings.NewReplacer("|", `\|`, "[", `\[`, "]", `\]`, "<", "&lt;", ">", "&gt;", "`", "\\`", "*", `\*`, "_", `\_`)

func mdEscape(s string) string { return mdReplacer.Replace(clean(s)) }

func truncate(s string, n int) string {
	if width(s) <= n {
		return s
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := runeWidth(r)
		if w+rw > n-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// width is the terminal display width of s (East Asian wide runes count as 2).
func width(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

func runeWidth(r rune) int {
	switch {
	case r == utf8.RuneError:
		return 1
	case r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe30 && r <= 0xfe4f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6)):
		return 2
	}
	return 1
}
