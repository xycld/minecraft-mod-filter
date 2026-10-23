// Package scan runs the full detection pipeline over a pack: local analysis,
// platform lookups, evidence resolution and dependency propagation.
package scan

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/xycld/minecraft-mod-filter/internal/cache"
	"github.com/xycld/minecraft-mod-filter/internal/detect"
	"github.com/xycld/minecraft-mod-filter/internal/inspect"
	"github.com/xycld/minecraft-mod-filter/internal/pack"
	"github.com/xycld/minecraft-mod-filter/internal/provider/curseforge"
	"github.com/xycld/minecraft-mod-filter/internal/provider/modrinth"
	"github.com/xycld/minecraft-mod-filter/internal/side"
)

type Options struct {
	Loader     detect.Loader      // "" = take from the pack or infer from the jars
	Modrinth   *modrinth.Client   // nil = offline
	CurseForge *curseforge.Client // nil = no CurseForge lookups
	Cache      *cache.Cache
	Download   bool // download pack-referenced files for local analysis
	Workers    int
	Progress   func(stage string, done, total int)
}

type ModrinthRef struct {
	ProjectID   string `json:"project_id"`
	Slug        string `json:"slug,omitempty"`
	Title       string `json:"title,omitempty"`
	VersionID   string `json:"version_id,omitempty"`
	Environment string `json:"environment,omitempty"`
	IconURL     string `json:"icon_url,omitempty"`
	ByHash      bool   `json:"by_hash"`
}

type CurseForgeRef struct {
	ModID  int    `json:"mod_id"`
	FileID int    `json:"file_id"`
	Name   string `json:"name,omitempty"`
	Slug   string `json:"slug,omitempty"`
}

// Mod is the result for one file.
type Mod struct {
	File       string          `json:"file"`
	Identity   detect.Identity `json:"identity"`
	SHA1       string          `json:"sha1,omitempty"`
	Size       int64           `json:"size,omitempty"`
	Verdict    detect.Verdict  `json:"verdict"`
	Modrinth   *ModrinthRef    `json:"modrinth,omitempty"`
	CurseForge *CurseForgeRef  `json:"curseforge,omitempty"`
	Depends    []string        `json:"depends,omitempty"`
	RequiredBy []string        `json:"required_by,omitempty"`
	Analyzed   bool            `json:"analyzed"` // the jar itself was inspected
	Problems   []string        `json:"problems,omitempty"`

	item     pack.Item
	jar      *inspect.Jar
	deps     []detect.Dependency
	evidence []detect.Evidence
}

type Result struct {
	Name        string   `json:"name,omitempty"`
	Format      string   `json:"format"`
	Loader      string   `json:"loader,omitempty"`
	GameVersion string   `json:"game_version,omitempty"`
	Online      bool     `json:"online"`
	Mods        []*Mod   `json:"mods"`
	Missing     []string `json:"missing_dependencies,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

func (o *Options) progress(stage string, done, total int) {
	if o.Progress != nil {
		o.Progress(stage, done, total)
	}
}

// Run scans a loaded pack.
func Run(ctx context.Context, pk *pack.Pack, opts Options) (*Result, error) {
	if opts.Workers <= 0 {
		opts.Workers = 8
	}
	res := &Result{
		Name: pk.Name, Format: pk.Format, Loader: pk.Loader, GameVersion: pk.GameVersion,
		Online: opts.Modrinth != nil, Warnings: pk.Warnings,
	}
	mods := make([]*Mod, len(pk.Items))
	for i, it := range pk.Items {
		mods[i] = &Mod{File: it.Path, item: it, SHA1: it.SHA1, Size: it.Size}
	}
	res.Mods = mods

	if opts.CurseForge != nil && pk.Format == "curseforge" {
		if err := resolveCurseForgeManifest(ctx, mods, opts); err != nil {
			res.Warnings = append(res.Warnings, "CurseForge: "+err.Error())
		}
	}

	// Local analysis. Each worker reads (or downloads) one jar, inspects it
	// and drops the bytes, so memory stays bounded by the worker count.
	var done atomic.Int32
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(opts.Workers)
	for _, m := range mods {
		g.Go(func() error {
			data, err := loadItem(gctx, m.item, opts.Download)
			if err != nil {
				mu.Lock()
				res.Warnings = append(res.Warnings, m.File+": "+err.Error())
				mu.Unlock()
			}
			if data != nil {
				m.jar = inspect.Open(m.item.Path, data)
				m.Analyzed = true
				m.SHA1, m.Size = m.jar.SHA1, m.jar.Size
				m.Problems = append(m.Problems, m.jar.Problems...)
			}
			opts.progress("analyze", int(done.Add(1)), len(mods))
			return nil
		})
	}
	g.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	switch {
	case opts.Loader != "":
		res.Loader = string(opts.Loader)
	case res.Loader == "":
		res.Loader = string(inferLoader(mods))
	}
	loader := detect.Loader(res.Loader)
	for _, m := range mods {
		if m.jar != nil {
			m.Identity = detect.IdentityOf(m.jar)
			m.deps = detect.DependenciesOf(m.jar, loader)
			m.evidence = append(m.evidence, detect.Local(m.jar, loader)...)
		} else {
			m.Identity = detect.Identity{Name: strings.TrimSuffix(m.File, ".jar")}
			m.evidence = append(m.evidence, detect.Evidence{Source: "jar", Rule: "fallback.both", Client: side.Required, Server: side.Required, Weight: detect.Fallback})
		}
		if d := m.item.Declared; d != nil {
			w := detect.Moderate
			if d.Client == side.Unsupported || d.Server == side.Unsupported {
				w = detect.Declared
			}
			m.evidence = append(m.evidence, detect.Evidence{Source: "pack", Rule: "pack.declared", Client: d.Client, Server: d.Server, Weight: w})
		}
	}

	if opts.Modrinth != nil {
		if err := lookupModrinth(ctx, mods, loader, res.GameVersion, opts); err != nil {
			res.Warnings = append(res.Warnings, "Modrinth: "+err.Error())
		}
	}
	if opts.CurseForge != nil {
		if err := lookupCurseForge(ctx, mods, opts); err != nil {
			res.Warnings = append(res.Warnings, "CurseForge: "+err.Error())
		}
	}

	for _, m := range mods {
		m.Verdict = detect.Resolve(m.evidence)
	}
	res.Missing = propagate(mods)
	sort.Slice(mods, func(i, j int) bool { return strings.ToLower(mods[i].File) < strings.ToLower(mods[j].File) })
	return res, nil
}

// inferLoader picks the loader most jars in a folder were built for. Jars
// are counted once per loader family, so a few multi-loader jars do not
// flip a Fabric folder to NeoForge.
func inferLoader(mods []*Mod) detect.Loader {
	var fabricFamily, quiltOnly, forgeFamily, neoforge, legacy int
	for _, m := range mods {
		if m.jar == nil {
			continue
		}
		has := map[detect.Loader]bool{}
		for _, l := range detect.LoadersOf(m.jar) {
			has[l] = true
		}
		if has[detect.Fabric] || has[detect.Quilt] {
			fabricFamily++
			if !has[detect.Fabric] {
				quiltOnly++
			}
		}
		switch {
		case has[detect.NeoForge] || has[detect.Forge]:
			forgeFamily++
			if has[detect.NeoForge] {
				neoforge++
			}
		case has[detect.LegacyForge]:
			legacy++
		}
	}
	switch {
	case fabricFamily == 0 && forgeFamily == 0:
		if legacy > 0 {
			return detect.LegacyForge
		}
		return ""
	case fabricFamily > forgeFamily:
		if quiltOnly > 0 {
			return detect.Quilt // Quilt runs Fabric mods; Fabric cannot run Quilt-only ones
		}
		return detect.Fabric
	case neoforge*2 >= forgeFamily:
		return detect.NeoForge
	}
	return detect.Forge
}

// Modrinth evidence weights. "client_and_server" is what Modrinth migrated
// every old project to, so it says less than an explicit one-sided value.
const (
	modrinthExplicit = detect.Strong + 5
	modrinthDefault  = detect.Strong - 10
	modrinthBySlug   = detect.Moderate + 5
	curseforgeTags   = detect.Strong - 5
)

func lookupModrinth(ctx context.Context, mods []*Mod, loader detect.Loader, gameVersion string, opts Options) error {
	versions := map[string]modrinth.Version{}
	var query []string
	for _, m := range mods {
		if m.SHA1 == "" {
			continue
		}
		var v modrinth.Version
		if found, hit := opts.Cache.Get("mr:sha1:"+m.SHA1, &v); found {
			if hit {
				versions[m.SHA1] = v
			}
			continue
		}
		query = append(query, m.SHA1)
	}
	if len(query) > 0 {
		opts.progress("modrinth", 0, len(query))
		got, err := opts.Modrinth.VersionsByHash(ctx, "sha1", dedupe(query))
		if err != nil {
			return err
		}
		for _, h := range query {
			if v, ok := got[h]; ok {
				versions[h] = v
				opts.Cache.Put("mr:sha1:"+h, v)
			} else {
				opts.Cache.Put("mr:sha1:"+h, nil)
			}
		}
	}

	projects := map[string]modrinth.Project{}
	var ids []string
	for _, v := range versions {
		var p modrinth.Project
		if found, hit := opts.Cache.Get("mr:project:"+v.ProjectID, &p); found && hit {
			projects[p.ID] = p
		} else {
			ids = append(ids, v.ProjectID)
		}
	}
	if len(ids) > 0 {
		sort.Strings(ids)
		got, err := opts.Modrinth.Projects(ctx, dedupe(ids))
		if err != nil {
			return err
		}
		for id, p := range got {
			projects[id] = p
			opts.Cache.Put("mr:project:"+id, p)
		}
	}

	var unmatched []*Mod
	for _, m := range mods {
		v, ok := versions[m.SHA1]
		if !ok {
			if m.jar != nil && len(m.Identity.ModIDs) > 0 {
				unmatched = append(unmatched, m)
			}
			continue
		}
		p := projects[v.ProjectID]
		m.Modrinth = &ModrinthRef{ProjectID: v.ProjectID, Slug: p.Slug, Title: p.Title, VersionID: v.ID, Environment: v.Environment, IconURL: p.IconURL, ByHash: true}
		if p.Title != "" && m.jar == nil {
			m.Identity.Name = p.Title
		}
		if m.Identity.Version == "" {
			m.Identity.Version = v.VersionNumber
		}
		if env, ok := side.FromModrinth(v.Environment); ok {
			w := modrinthExplicit
			if v.Environment == "client_and_server" {
				w = modrinthDefault
			}
			m.evidence = append(m.evidence, detect.Evidence{Source: "modrinth", Rule: "modrinth.environment", Args: []string{v.Environment}, Client: env.Client, Server: env.Server, Weight: w})
		} else if env, ok := side.FromLegacyModrinth(p.ClientSide, p.ServerSide); ok {
			env = normalizeLegacy(env)
			m.evidence = append(m.evidence, detect.Evidence{Source: "modrinth", Rule: "modrinth.project", Args: []string{p.ClientSide, p.ServerSide}, Client: env.Client, Server: env.Server, Weight: modrinthDefault})
		}
	}

	// Files not on Modrinth (repacked, CurseForge-only, dev builds): try the
	// project whose slug equals the mod id and accept it only if it is a mod
	// with a matching name.
	var mu sync.Mutex
	var failed []string
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	for _, m := range unmatched {
		g.Go(func() error {
			p, err := lookupBySlug(gctx, m, opts)
			var env string
			var ok bool
			if err == nil && p != nil {
				env, ok, err = projectEnv(gctx, p, loader, gameVersion, opts)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, m.File)
				return nil
			}
			if p == nil {
				return nil
			}
			m.Modrinth = &ModrinthRef{ProjectID: p.ID, Slug: p.Slug, Title: p.Title, IconURL: p.IconURL, Environment: env}
			if e, ok2 := side.FromModrinth(env); ok && ok2 {
				m.evidence = append(m.evidence, detect.Evidence{Source: "modrinth", Rule: "modrinth.slug", Args: []string{p.Slug, env}, Client: e.Client, Server: e.Server, Weight: modrinthBySlug})
			}
			return nil
		})
	}
	g.Wait()
	if len(failed) > 0 {
		sort.Strings(failed)
		return fmt.Errorf("lookup by mod id failed for %d file(s): %s", len(failed), strings.Join(failed, ", "))
	}
	return nil
}

// normalizeLegacy maps the old project-level "unsupported" (which authors used
// for "not needed") onto optional when the other side is required.
func normalizeLegacy(e side.Env) side.Env {
	if e.Client == side.Unsupported && e.Server == side.Required {
		e.Client = side.Optional
	}
	return e
}

func lookupBySlug(ctx context.Context, m *Mod, opts Options) (*modrinth.Project, error) {
	id := m.Identity.ModIDs[0]
	for _, slug := range dedupe([]string{id, strings.ReplaceAll(id, "_", "-")}) {
		key := "mr:slug:" + slug
		var p modrinth.Project
		if found, hit := opts.Cache.Get(key, &p); found {
			if hit && plausible(&p, m) {
				return &p, nil
			}
			continue
		}
		got, err := opts.Modrinth.Project(ctx, slug)
		if err != nil {
			return nil, err
		}
		if got == nil {
			opts.Cache.Put(key, nil)
			continue
		}
		opts.Cache.Put(key, got)
		if plausible(got, m) {
			return got, nil
		}
	}
	return nil, nil
}

func plausible(p *modrinth.Project, m *Mod) bool {
	if p.ProjectType != "mod" {
		return false
	}
	a, b := normalize(p.Title), normalize(m.Identity.Name)
	return a != "" && b != "" && (strings.Contains(a, b) || strings.Contains(b, a))
}

func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// projectEnv returns the environment of the project's newest version for the
// pack's loader and game version, falling back to any version.
func projectEnv(ctx context.Context, p *modrinth.Project, loader detect.Loader, gameVersion string, opts Options) (string, bool, error) {
	key := fmt.Sprintf("mr:env:%s:%s:%s", p.ID, loader, gameVersion)
	var env string
	if found, hit := opts.Cache.Get(key, &env); found {
		return env, hit, nil
	}
	l := string(loader)
	if loader == detect.LegacyForge {
		l = "forge"
	}
	tried := map[[2]string]bool{}
	for _, try := range [][2]string{{l, gameVersion}, {l, ""}, {"", ""}} {
		if tried[try] {
			continue
		}
		tried[try] = true
		vs, err := opts.Modrinth.ProjectVersions(ctx, p.ID, try[0], try[1])
		if err != nil {
			return "", false, err
		}
		for _, v := range vs {
			if _, ok := side.FromModrinth(v.Environment); ok {
				opts.Cache.Put(key, v.Environment)
				return v.Environment, true, nil
			}
		}
	}
	opts.Cache.Put(key, nil)
	return "", false, nil
}

func lookupCurseForge(ctx context.Context, mods []*Mod, opts Options) error {
	files := map[uint32]curseforge.File{}
	var query []uint32
	for _, m := range mods {
		if m.jar == nil || m.CurseForge != nil {
			continue // already identified from the pack manifest
		}
		fp := m.jar.Fingerprint
		var f curseforge.File
		if found, hit := opts.Cache.Get(fmt.Sprintf("cf:fp:%d", fp), &f); found {
			if hit {
				files[fp] = f
			}
			continue
		}
		query = append(query, fp)
	}
	if len(query) > 0 {
		got, err := opts.CurseForge.FilesByFingerprint(ctx, query)
		if err != nil {
			return err
		}
		for _, fp := range query {
			if f, ok := got[fp]; ok {
				files[fp] = f
				opts.Cache.Put(fmt.Sprintf("cf:fp:%d", fp), f)
			} else {
				opts.Cache.Put(fmt.Sprintf("cf:fp:%d", fp), nil)
			}
		}
	}
	for _, m := range mods {
		if m.jar == nil || m.CurseForge != nil {
			continue
		}
		f, ok := files[m.jar.Fingerprint]
		if !ok {
			continue
		}
		m.CurseForge = &CurseForgeRef{ModID: f.ModID, FileID: f.ID, Name: f.DisplayName}
		if env, ok := f.Env(); ok {
			m.evidence = append(m.evidence, detect.Evidence{Source: "curseforge", Rule: "curseforge.tags", Client: env.Client, Server: env.Server, Weight: curseforgeTags})
		}
	}
	return nil
}

// resolveCurseForgeManifest fills in hashes and download URLs for the files
// a CurseForge modpack manifest references.
func resolveCurseForgeManifest(ctx context.Context, mods []*Mod, opts Options) error {
	var ids, modIDs []int
	for _, m := range mods {
		if m.item.CurseForgeFile != 0 {
			ids = append(ids, m.item.CurseForgeFile)
			modIDs = append(modIDs, m.item.CurseForgeProject)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	files, err := opts.CurseForge.Files(ctx, ids)
	if err != nil {
		return err
	}
	projects, _ := opts.CurseForge.Mods(ctx, modIDs)
	for _, m := range mods {
		f, ok := files[m.item.CurseForgeFile]
		if !ok {
			continue
		}
		m.File = f.FileName
		m.item.Path = f.FileName
		m.SHA1 = f.SHA1()
		m.item.SHA1 = m.SHA1
		if f.DownloadURL != "" {
			m.item.URLs = []string{f.DownloadURL}
		}
		p := projects[f.ModID]
		m.CurseForge = &CurseForgeRef{ModID: f.ModID, FileID: f.ID, Name: p.Name, Slug: p.Slug}
		if env, ok := f.Env(); ok {
			m.evidence = append(m.evidence, detect.Evidence{Source: "curseforge", Rule: "curseforge.tags", Client: env.Client, Server: env.Server, Weight: curseforgeTags})
		}
	}
	return nil
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	out := s[:0:0]
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func sha1Hex(b []byte) string {
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:])
}
