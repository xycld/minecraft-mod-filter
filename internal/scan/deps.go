package scan

import (
	"sort"
	"strings"

	"github.com/xycld/minecraft-mod-filter/internal/detect"
	"github.com/xycld/minecraft-mod-filter/internal/side"
)

// Mod ids that are satisfied by the platform rather than by a mod file.
var builtinIDs = map[string]bool{
	"minecraft": true, "java": true, "forge": true, "neoforge": true, "fabricloader": true,
	"fabric_loader": true, "quilt_loader": true, "mixinextras": true, "fml": true,
}

// Alternative ids for the same mod across loaders (keys and values normalised).
var aliases = map[string]string{
	"fabric": "fabric_api", // the pre-1.19 Fabric API id
}

// requiredByWeight beats author-declared platform data: a loader refuses to
// start when a mandatory dependency is missing.
const (
	requiredByWeight = detect.Declared - 5
	orphanWeight     = detect.Strong - 5
)

// normID makes ids comparable across loaders: Forge forbids "-" in mod ids,
// so ports such as Forgified Fabric API and Sinytra Connector use "_".
func normID(id string) string {
	id = strings.ReplaceAll(strings.ToLower(id), "-", "_")
	if a, ok := aliases[id]; ok {
		return a
	}
	return id
}

type edge struct {
	from *Mod   // the dependent
	to   *Mod   // the dependency's provider
	side string // "BOTH", "CLIENT" or "SERVER"
}

// propagate applies two pack-level rules and returns dependency ids no file
// in the pack provides:
//   - a mandatory dependency of a mod needed on a side is needed there too;
//   - a library that no server-required mod depends on, and whose own claim
//     to the server is weak, is optional on the server.
//
// Only standalone jars provide dependencies. A jar that merely bundles a
// library (jar-in-jar) satisfies the dependency for the missing-dependency
// check but is never made required on its behalf.
func propagate(mods []*Mod) []string {
	providers := map[string][]*Mod{}
	bundled := map[string]bool{}
	for _, m := range mods {
		for _, id := range m.Identity.ModIDs {
			providers[normID(id)] = append(providers[normID(id)], m)
		}
		for _, id := range m.Identity.BundledIDs {
			bundled[normID(id)] = true
		}
	}
	var edges []edge
	missing := map[string]bool{}
	for _, m := range mods {
		for _, d := range m.deps {
			id := normID(d.ModID)
			if len(providers[id]) == 0 && !bundled[id] && !builtinIDs[id] {
				missing[d.ModID] = true
			}
			for _, p := range providers[id] {
				if p != m {
					edges = append(edges, edge{from: m, to: p, side: d.Side})
				}
			}
		}
	}
	incoming := map[*Mod][]edge{}
	for _, e := range edges {
		incoming[e.to] = append(incoming[e.to], e)
	}

	applied := map[[3]any]bool{}
	for round := 0; round < 32; round++ {
		changed := false
		for _, e := range edges {
			if e.from.Verdict.Env.Server == side.Required && e.side != "CLIENT" {
				changed = require(e.to, e.from, "server", applied) || changed
			}
			if e.from.Verdict.Env.Client == side.Required && e.side != "SERVER" {
				changed = require(e.to, e.from, "client", applied) || changed
			}
		}
		for _, m := range mods {
			if in := incoming[m]; len(in) > 0 {
				changed = orphan(m, in, applied) || changed
			}
		}
		if !changed {
			break
		}
	}

	for _, m := range mods {
		for _, d := range m.deps {
			m.Depends = append(m.Depends, d.ModID)
		}
	}
	for _, e := range edges {
		if !contains(e.to.RequiredBy, e.from.Identity.Name) {
			e.to.RequiredBy = append(e.to.RequiredBy, e.from.Identity.Name)
		}
	}
	for _, m := range mods {
		sort.Strings(m.RequiredBy)
	}
	var out []string
	for id := range missing {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// require adds evidence that p is needed on a side because m needs it there.
// The edge is remembered only once evidence is attached, so a library that is
// already required (and might later be lowered by orphan) is revisited.
func require(p, m *Mod, s string, applied map[[3]any]bool) bool {
	key := [3]any{p, m, s}
	if applied[key] {
		return false
	}
	cur := p.Verdict.Env.Server
	if s == "client" {
		cur = p.Verdict.Env.Client
	}
	if cur == side.Required {
		return false
	}
	applied[key] = true
	e := detect.Evidence{Source: "deps", Rule: "deps.requiredBy." + s, Args: []string{m.Identity.Name}, Weight: requiredByWeight}
	if s == "server" {
		e.Server = side.Required
	} else {
		e.Client = side.Required
	}
	p.evidence = append(p.evidence, e)
	p.Verdict = detect.Resolve(p.evidence)
	if (s == "server" && p.Verdict.Env.Server != side.Required) || (s == "client" && p.Verdict.Env.Client != side.Required) {
		p.Problems = append(p.Problems, "required on the "+s+" by "+m.Identity.Name+", but evidence says otherwise")
	}
	return true
}

// orphan marks a library as optional on the server when no mod depending on
// it needs it there. Libraries with their own strong server evidence
// (content, worldgen, explicit platform tags) are left alone.
func orphan(p *Mod, in []edge, applied map[[3]any]bool) bool {
	key := [3]any{p, nil, "orphan"}
	if applied[key] || p.Verdict.Env.Server != side.Required {
		return false
	}
	var names []string
	for _, e := range in {
		if e.side != "CLIENT" && e.from.Verdict.Env.Server == side.Required {
			return false
		}
		names = append(names, e.from.Identity.Name)
	}
	for _, e := range p.evidence {
		if e.Server == side.Required && e.Weight >= orphanWeight {
			return false
		}
	}
	applied[key] = true
	sort.Strings(names)
	p.evidence = append(p.evidence, detect.Evidence{Source: "deps", Rule: "deps.notNeededOnServer",
		Args: []string{strings.Join(dedupe(names), ", ")}, Server: side.Optional, Weight: orphanWeight})
	p.Verdict = detect.Resolve(p.evidence)
	return true
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
