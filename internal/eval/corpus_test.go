// Package eval measures detection accuracy against a labelled corpus of real
// mod jars in testdata/corpus/<set>/ (not committed; see tools/fetchcorpus).
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/xycld/minecraft-mod-filter/internal/detect"
	"github.com/xycld/minecraft-mod-filter/internal/inspect"
	"github.com/xycld/minecraft-mod-filter/internal/side"
)

type label struct {
	File        string    `json:"file"`
	Environment string    `json:"environment"`
	Expected    *side.Env `json:"expected"`
}

func (l label) env() (side.Env, bool) {
	if l.Expected != nil {
		return *l.Expected, true
	}
	return side.FromModrinth(l.Environment)
}

// setLoader infers the target loader from a set name like "fabric-1.20.1".
func setLoader(set string) detect.Loader {
	if set == "smallish-bits" {
		return detect.NeoForge
	}
	l, _, _ := strings.Cut(set, "-")
	return detect.Loader(l)
}

type score struct{ n, decision, serverKeep, clientKeep, strict int }

// decisionOK reports whether acting on got would be harmless given want:
// a required mod must be kept, an unsupported one must not be required, and
// either choice is fine for an optional one.
func decisionOK(got, want side.Need) bool {
	switch want {
	case side.Required:
		return got == side.Required
	case side.Unsupported:
		return got != side.Required
	}
	return true
}

func (s *score) add(got, want side.Env) bool {
	s.n++
	if decisionOK(got.Server, want.Server) && decisionOK(got.Client, want.Client) {
		s.decision++
	}
	sk := got.Server.Keep() == want.Server.Keep()
	ck := got.Client.Keep() == want.Client.Keep()
	if sk {
		s.serverKeep++
	}
	if ck {
		s.clientKeep++
	}
	if got == want {
		s.strict++
	}
	return sk && ck
}

func (s score) String() string {
	pct := func(k int) float64 { return 100 * float64(k) / float64(max(s.n, 1)) }
	return fmt.Sprintf("n=%3d  decision %5.1f%%  server-keep %5.1f%%  client-keep %5.1f%%  exact %5.1f%%",
		s.n, pct(s.decision), pct(s.serverKeep), pct(s.clientKeep), pct(s.strict))
}

// TestCorpusOffline reports how well local analysis alone matches the labels.
// testdata/corpus is the set the rules were tuned on; testdata/holdout holds
// Minecraft versions never used for tuning, to catch overfitting.
func TestCorpusOffline(t *testing.T) {
	ran := false
	for _, root := range []string{"corpus", "holdout"} {
		sets, _ := filepath.Glob("../../testdata/" + root + "/*/labels.json")
		if len(sets) == 0 {
			continue
		}
		ran = true
		var total score
		for _, labelsPath := range sets {
			s := evalSet(t, labelsPath)
			total.n += s.n
			total.decision += s.decision
			total.serverKeep += s.serverKeep
			total.clientKeep += s.clientKeep
			total.strict += s.strict
		}
		t.Logf("%-18s %s", strings.ToUpper(root), total)
	}
	if !ran {
		t.Skip("no corpus in testdata/corpus or testdata/holdout")
	}
}

func evalSet(t *testing.T, labelsPath string) score {
	dir := filepath.Dir(labelsPath)
	set := filepath.Base(dir)
	var labels []label
	raw, err := os.ReadFile(labelsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &labels); err != nil {
		t.Fatalf("%s: %v", labelsPath, err)
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i].File < labels[j].File })
	var s score
	var misses []string
	for _, l := range labels {
		want, ok := l.env()
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, l.File))
		if err != nil {
			continue
		}
		v := detect.Resolve(detect.Local(inspect.Open(l.File, data), setLoader(set)))
		s.add(v.Env, want)
		if !decisionOK(v.Env.Server, want.Server) || !decisionOK(v.Env.Client, want.Client) {
			misses = append(misses, fmt.Sprintf("  %-55.55s want %-26s got %-26s %-6s %s",
				l.File, want.Category(), v.Env.Category(), v.Confidence, rules(v)))
		}
	}
	t.Logf("%-18s %s", set, s)
	if testing.Verbose() {
		for _, m := range misses {
			t.Log(m)
		}
	}
	return s
}

func rules(v detect.Verdict) string {
	var parts []string
	for _, e := range v.Evidence {
		parts = append(parts, fmt.Sprintf("%s(%d)", e.Rule, e.Weight))
	}
	return strings.Join(parts, " ")
}
