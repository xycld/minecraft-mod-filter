// Package detect turns facts about a mod into a client/server verdict.
//
// Every signal becomes an Evidence: a claim about the client and/or server
// need, with a weight. Resolve picks, per side, the claim with the highest
// weight, so a hard loader rule always beats a heuristic and every verdict can
// be traced back to the evidence that produced it.
package detect

import (
	"sort"

	"github.com/xycld/minecraft-mod-filter/internal/side"
)

// Evidence weights. Higher always wins.
const (
	Fallback = 10  // nothing else known
	Weak     = 25  // loose bytecode statistics
	Moderate = 45  // clear but indirect signals
	Strong   = 70  // explicit but not loader-enforced metadata, author-declared provider data
	Declared = 85  // pack author's or user's explicit choice
	Hard     = 100 // enforced by the mod loader
)

// Evidence is one claim about where a mod belongs.
type Evidence struct {
	Source string    `json:"source"` // "jar", "modrinth", "curseforge", "pack", "deps", "user"
	Rule   string    `json:"rule"`   // stable identifier, also the i18n message key
	Args   []string  `json:"args,omitempty"`
	Client side.Need `json:"client"` // Unknown = no claim about this side
	Server side.Need `json:"server"`
	Weight int       `json:"weight"`
}

// Confidence of a verdict.
type Confidence string

const (
	High   Confidence = "high"
	Medium Confidence = "medium"
	Low    Confidence = "low"
)

// Verdict is the resolved placement of a mod.
type Verdict struct {
	Env        side.Env   `json:"env"`
	Category   string     `json:"category"`
	Confidence Confidence `json:"confidence"`
	Evidence   []Evidence `json:"evidence"`
}

// Resolve combines evidence into a verdict.
func Resolve(evs []Evidence) Verdict {
	sorted := append([]Evidence(nil), evs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Weight > sorted[j].Weight })

	client, cw, cConflict := pick(sorted, func(e Evidence) side.Need { return e.Client })
	server, sw, sConflict := pick(sorted, func(e Evidence) side.Need { return e.Server })
	env := side.Env{Client: client, Server: server}

	// A mod that runs nowhere is a contradiction; trust the stronger side.
	if env.Client == side.Unsupported && env.Server == side.Unsupported {
		if cw >= sw {
			env.Server = side.Optional
		} else {
			env.Client = side.Optional
		}
	}

	top := min(cw, sw)
	if env.Client == side.Unknown {
		top = sw
	} else if env.Server == side.Unknown {
		top = cw
	}
	conf := Low
	switch {
	case top >= Strong:
		conf = High
	case top >= Moderate:
		conf = Medium
	}
	if (cConflict || sConflict) && conf == High {
		conf = Medium
	}
	return Verdict{Env: env, Category: env.Category(), Confidence: conf, Evidence: sorted}
}

// pick returns the need with the highest single weight; ties are broken by
// the summed weight of each candidate, then toward keeping the mod.
// conflict reports whether a different need was claimed at a close weight.
func pick(evs []Evidence, get func(Evidence) side.Need) (side.Need, int, bool) {
	best := map[side.Need]int{}
	sum := map[side.Need]int{}
	for _, e := range evs {
		n := get(e)
		if n == side.Unknown {
			continue
		}
		best[n] = max(best[n], e.Weight)
		sum[n] += e.Weight
	}
	var choice side.Need
	for _, n := range []side.Need{side.Required, side.Optional, side.Unsupported} {
		w, ok := best[n]
		if !ok {
			continue
		}
		if choice == side.Unknown || w > best[choice] || (w == best[choice] && sum[n] > sum[choice]) {
			choice = n
		}
	}
	conflict := false
	for n, w := range best {
		if n != choice && w > best[choice]-10 && !compatible(n, choice) {
			conflict = true
		}
	}
	return choice, best[choice], conflict
}

// compatible needs differ only in strength, not in whether to install.
func compatible(a, b side.Need) bool {
	return (a == side.Required || a == side.Optional) && (b == side.Required || b == side.Optional)
}
