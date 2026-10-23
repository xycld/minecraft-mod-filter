package detect

import (
	"path"
	"sort"
	"strings"

	"github.com/xycld/minecraft-mod-filter/internal/inspect"
	"github.com/xycld/minecraft-mod-filter/internal/meta"
)

// Loader names a mod loader whose metadata a jar carries.
type Loader string

const (
	Fabric      Loader = "fabric"
	Quilt       Loader = "quilt"
	Forge       Loader = "forge"
	NeoForge    Loader = "neoforge"
	LegacyForge Loader = "forge-legacy" // mcmod.info era (1.12 and older)
)

// Identity is what a jar calls itself.
type Identity struct {
	Name       string   `json:"name"`
	Version    string   `json:"version,omitempty"`
	ModIDs     []string `json:"mod_ids,omitempty"`     // ids this jar provides itself
	BundledIDs []string `json:"bundled_ids,omitempty"` // ids of mods bundled inside it (jar-in-jar)
	Loaders    []Loader `json:"loaders,omitempty"`
}

// Dependency is a mandatory dependency on another mod.
type Dependency struct {
	ModID string
	Side  string // "BOTH", "CLIENT", "SERVER"
}

var platformIDs = map[string]bool{
	"minecraft": true, "java": true, "forge": true, "neoforge": true,
	"fabricloader": true, "fabric-loader": true, "quilt_loader": true, "javafml": true,
}

func IdentityOf(j *inspect.Jar) Identity {
	id := Identity{Name: strings.TrimSuffix(path.Base(j.Name), ".jar")}
	seen := map[string]bool{}
	var walk func(*inspect.Jar, bool)
	top := true
	addID := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] || strings.Contains(s, "${") {
			return
		}
		seen[s] = true
		if top {
			id.ModIDs = append(id.ModIDs, s)
		} else {
			id.BundledIDs = append(id.BundledIDs, s)
		}
	}
	setName := func(name, version string) {
		if name != "" && id.Name == strings.TrimSuffix(path.Base(j.Name), ".jar") {
			id.Name = name
		}
		if version != "" && id.Version == "" && !strings.Contains(version, "${") {
			id.Version = version
		}
	}

	walk = func(j *inspect.Jar, isTop bool) {
		top = isTop
		if m := j.Fabric; m != nil {
			addID(m.ID)
			for _, p := range m.Provides {
				addID(p)
			}
			if top {
				setName(m.Name, m.Version)
			}
		}
		if m := j.Quilt; m != nil {
			addID(m.ID)
			for _, p := range m.Provides {
				addID(p)
			}
			if top {
				setName(m.Name, m.Version)
			}
		}
		for _, t := range []*meta.ModsToml{j.NeoToml, j.ForgeToml} {
			if t == nil {
				continue
			}
			for _, m := range t.Mods {
				addID(m.ModID)
				if top {
					setName(m.DisplayName, m.Version)
				}
			}
		}
		for _, m := range j.Mcmod {
			addID(m.ModID)
			if top {
				setName(m.Name, m.Version)
			}
		}
		for _, m := range j.Classes.ModAnnotations {
			addID(m.ModID)
		}
		for _, n := range j.Nested {
			walk(n, false)
		}
		top = isTop
	}
	walk(j, true)
	if id.Version == "" && j.Manifest != nil {
		id.Version = j.Manifest["Implementation-Version"]
	}
	id.Loaders = LoadersOf(j)
	return id
}

// LoadersOf lists the loaders a jar has metadata for.
func LoadersOf(j *inspect.Jar) []Loader {
	var ls []Loader
	if j.Fabric != nil {
		ls = append(ls, Fabric)
	}
	if j.Quilt != nil {
		ls = append(ls, Quilt)
	}
	if j.NeoToml != nil {
		ls = append(ls, NeoForge)
	}
	if j.ForgeToml != nil {
		ls = append(ls, Forge)
	}
	if len(j.Mcmod) > 0 || (len(ls) == 0 && len(j.Classes.ModAnnotations) > 0) {
		ls = append(ls, LegacyForge)
	}
	return ls
}

// MetadataFilter reports which loaders' metadata in j applies when the pack
// runs on target. A jar with metadata for the target uses only that metadata;
// otherwise everything counts (a Fabric mod on Quilt, or on NeoForge through
// Sinytra Connector). NeoForge 1.20.1 still read META-INF/mods.toml.
func MetadataFilter(j *inspect.Jar, target Loader) func(Loader) bool {
	has := map[Loader]bool{}
	for _, l := range LoadersOf(j) {
		has[l] = true
	}
	switch {
	case target == NeoForge && !has[NeoForge] && has[Forge]:
		target = Forge
	case target == Quilt && !has[Quilt] && has[Fabric]:
		target = Fabric
	}
	return func(l Loader) bool { return target == "" || !has[target] || l == target }
}

// DependenciesOf lists mandatory dependencies on other mods (platform ids
// excluded). For multi-loader jars only the target loader's metadata counts;
// a jar without metadata for the target (e.g. a Fabric mod run through
// Connector on NeoForge) contributes all of its metadata.
func DependenciesOf(j *inspect.Jar, target Loader) []Dependency {
	use := MetadataFilter(j, target)
	seen := map[string]bool{}
	var deps []Dependency
	add := func(id, s string) {
		id = strings.ToLower(id)
		if id == "" || platformIDs[id] || seen[id] {
			return
		}
		if s == "" {
			s = "BOTH"
		}
		seen[id] = true
		deps = append(deps, Dependency{ModID: id, Side: s})
	}
	if j.Fabric != nil && use(Fabric) {
		for _, d := range j.Fabric.Depends {
			add(d, "BOTH")
		}
	}
	if j.Quilt != nil && use(Quilt) {
		for _, d := range j.Quilt.Depends {
			add(d, "BOTH")
		}
	}
	for _, t := range []*meta.ModsToml{j.NeoToml, j.ForgeToml} {
		if t == nil || !use(tomlLoader(t)) {
			continue
		}
		for _, list := range t.Dependencies {
			for _, d := range list {
				if d.Mandatory {
					add(d.ModID, d.Side)
				}
			}
		}
	}
	sort.Slice(deps, func(a, b int) bool { return deps[a].ModID < deps[b].ModID })
	return deps
}

func tomlLoader(t *meta.ModsToml) Loader {
	if t.NeoForge {
		return NeoForge
	}
	return Forge
}
