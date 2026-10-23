// Package inspect reads a mod jar in memory and extracts every fact that is
// useful for deciding which side the mod belongs on. It never extracts files
// to disk and never executes anything from the jar.
package inspect

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/xycld/minecraft-mod-filter/internal/classfile"
	"github.com/xycld/minecraft-mod-filter/internal/meta"
)

const (
	maxNestedDepth = 4
	maxEntrySize   = 32 << 20  // a single class or metadata file
	maxNestedSize  = 128 << 20 // a single bundled jar
	// Per top-level jar: bounds memory and work for hostile jar-in-jar nesting.
	maxNestedTotal = 256 << 20
	maxNestedCount = 256
)

// Jar holds everything learned from one jar file.
type Jar struct {
	Name        string // file name, or the path inside the parent for nested jars
	Size        int64
	SHA1        string
	SHA512      string
	Fingerprint uint32 // CurseForge fingerprint

	Fabric    *meta.FabricMod // fabric.mod.json
	Quilt     *meta.FabricMod // quilt.mod.json
	NeoToml   *meta.ModsToml  // META-INF/neoforge.mods.toml
	ForgeToml *meta.ModsToml  // META-INF/mods.toml
	Mcmod     []meta.McmodInfo
	Manifest  map[string]string

	Mixins  []Mixin
	Classes ClassStats
	Assets  AssetStats
	Nested  []*Jar

	Problems []string
}

// Mixin summarises one mixin config.
type Mixin struct {
	Config      string
	Environment string // environment from the referencing metadata: "*", "client", "server"
	Common      int
	Client      int
	Server      int
	// CommonOnClient counts "common" mixins whose targets are all client classes.
	CommonOnClient int
}

// ClassStats summarises the bytecode of a jar (nested jars excluded).
type ClassStats struct {
	Total int
	// Game counts non-mixin classes that touch Minecraft or a mod loader API.
	Game int
	// Client counts Game classes that are client-only: they reference
	// client classes, carry a client-only annotation or live in a client package.
	Client int
	// Server counts classes that reference dedicated/integrated-server classes.
	Server int

	Registers      bool // uses registry APIs to add content
	Networking     bool // registers network payloads/channels
	Commands       bool // registers commands
	MixinClasses   int
	ModAnnotations []ModAnnotation
}

// ModAnnotation is a Forge/NeoForge @Mod class annotation.
type ModAnnotation struct {
	Class                    string
	ModID                    string
	Dist                     []string // NeoForge: "CLIENT", "DEDICATED_SERVER"
	ClientSideOnly           bool     // Forge 1.12
	ServerSideOnly           bool     // Forge 1.12
	AcceptableRemoteVersions string   // Forge 1.12; "*" means the other side may lack the mod
}

// AssetStats counts resource files by kind.
type AssetStats struct {
	Textures, Models, Blockstates, Sounds, Shaders, Lang int
	Data, Worldgen                                       int
	// Resources in the mod's own namespaces (not "minecraft") that only make
	// sense when the mod registers blocks or items on both sides.
	OwnBlockstates, OwnItemModels, OwnRecipesLoot int
}

// AddsContent reports whether the resources describe new blocks or items.
func (a AssetStats) AddsContent() bool {
	return a.OwnBlockstates > 0 || (a.OwnItemModels > 0 && a.OwnRecipesLoot > 0)
}

// ClientResources reports whether the jar ships client resources beyond translations.
func (a AssetStats) ClientResources() bool {
	return a.Textures+a.Models+a.Blockstates+a.Sounds+a.Shaders > 0
}

// budget limits how much bundled-jar data one top-level jar may expand to.
type budget struct {
	bytes int64
	count int
}

// Open reads a jar from memory. Hashes are computed for the top-level jar
// only; they identify the file on mod platforms.
func Open(name string, data []byte) *Jar {
	s1 := sha1.Sum(data)
	s512 := sha512.Sum512(data)
	j := &Jar{
		Name:        name,
		Size:        int64(len(data)),
		SHA1:        hex.EncodeToString(s1[:]),
		SHA512:      hex.EncodeToString(s512[:]),
		Fingerprint: CurseForgeFingerprint(data),
	}
	j.open(data, 0, &budget{bytes: maxNestedTotal, count: maxNestedCount})
	return j
}

func (j *Jar) open(data []byte, depth int, b *budget) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		j.problem("not a valid zip/jar: %v", err)
		return
	}
	j.read(zr, depth, b)
}

func (j *Jar) problem(format string, args ...any) {
	j.Problems = append(j.Problems, fmt.Sprintf(format, args...))
}

type classInfo struct {
	name         string
	client       bool // client-only by reference, annotation or package
	game         bool
	mixinTargets []string
	isMixin      bool
}

func (j *Jar) read(zr *zip.Reader, depth int, b *budget) {
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		name := f.Name
		if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			continue // never trust odd paths; we only read, but keep lookups sane
		}
		files[name] = f
	}
	readFile := func(name string) []byte {
		f, ok := files[name]
		if !ok {
			return nil
		}
		b, err := readEntry(f)
		if err != nil {
			j.problem("%s: %v", name, err)
			return nil
		}
		return b
	}

	if b := readFile("fabric.mod.json"); b != nil {
		if m, err := meta.ParseFabric(b); err == nil {
			j.Fabric = m
		} else {
			j.problem("fabric.mod.json: %v", err)
		}
	}
	if b := readFile("quilt.mod.json"); b != nil {
		if m, err := meta.ParseQuilt(b); err == nil {
			j.Quilt = m
		} else {
			j.problem("quilt.mod.json: %v", err)
		}
	}
	if b := readFile("META-INF/neoforge.mods.toml"); b != nil {
		j.NeoToml, _ = meta.ParseModsToml(b, true)
	}
	if b := readFile("META-INF/mods.toml"); b != nil {
		j.ForgeToml, _ = meta.ParseModsToml(b, false)
	}
	if b := readFile("mcmod.info"); b != nil {
		if list, err := meta.ParseMcmodInfo(b); err == nil {
			j.Mcmod = list
		} else {
			j.problem("mcmod.info: %v", err)
		}
	}
	if b := readFile("META-INF/MANIFEST.MF"); b != nil {
		j.Manifest = meta.ParseManifest(b)
	}

	classes := map[string]*classInfo{}
	var nested []string
	for name, f := range files {
		switch {
		case strings.HasSuffix(name, ".class"):
			if strings.HasPrefix(name, "META-INF/versions/") {
				continue // multi-release duplicates
			}
			b, err := readEntry(f)
			if err != nil {
				j.problem("%s: %v", name, err)
				continue
			}
			c, err := classfile.Parse(b)
			if err != nil {
				continue // obfuscated or corrupt classes are not worth a warning each
			}
			j.Classes.Total++
			classes[c.Name] = j.classify(c)
		case strings.HasSuffix(name, ".jar") && strings.HasPrefix(name, "META-INF/"):
			nested = append(nested, name)
		case strings.HasPrefix(name, "assets/"):
			j.Assets.count(name)
		case strings.HasPrefix(name, "data/"):
			j.Assets.countData(name)
		}
	}
	for _, c := range classes {
		if c.isMixin {
			j.Classes.MixinClasses++
			continue
		}
		if c.game {
			j.Classes.Game++
			if c.client {
				j.Classes.Client++
			}
		}
	}

	for _, ref := range j.mixinRefs(files) {
		b := readFile(ref.Config)
		if b == nil {
			continue
		}
		cfg, err := meta.ParseMixinConfig(b)
		if err != nil {
			j.problem("%s: %v", ref.Config, err)
			continue
		}
		mx := Mixin{Config: ref.Config, Environment: ref.Environment, Common: len(cfg.Common), Client: len(cfg.Client), Server: len(cfg.Server)}
		for _, name := range cfg.Common {
			c := classes[strings.TrimSuffix(cfg.ClassPath(name), ".class")]
			if c != nil && len(c.mixinTargets) > 0 && allClient(c.mixinTargets) {
				mx.CommonOnClient++
			}
		}
		j.Mixins = append(j.Mixins, mx)
	}

	sort.Slice(j.Classes.ModAnnotations, func(a, c int) bool {
		return j.Classes.ModAnnotations[a].Class < j.Classes.ModAnnotations[c].Class
	})

	if depth >= maxNestedDepth {
		return
	}
	sort.Strings(nested)
	for _, name := range nested {
		f := files[name]
		size := int64(f.UncompressedSize64)
		if b.count <= 0 || size > maxNestedSize || size > b.bytes {
			j.problem("%s: bundled jar skipped (size or count limit)", name)
			continue
		}
		b.count--
		b.bytes -= size
		data, err := readLimited(f, maxNestedSize)
		if err != nil {
			j.problem("%s: %v", name, err)
			continue
		}
		n := &Jar{Name: name, Size: int64(len(data))}
		n.open(data, depth+1, b)
		j.Nested = append(j.Nested, n)
	}
}

// mixinRefs gathers mixin configs from every metadata source, deduplicated.
func (j *Jar) mixinRefs(files map[string]*zip.File) []meta.MixinRef {
	seen := map[string]bool{}
	var refs []meta.MixinRef
	add := func(cfg, env string) {
		if cfg == "" || seen[cfg] {
			return
		}
		seen[cfg] = true
		refs = append(refs, meta.MixinRef{Config: cfg, Environment: env})
	}
	for _, m := range []*meta.FabricMod{j.Fabric, j.Quilt} {
		if m != nil {
			for _, r := range m.Mixins {
				add(r.Config, r.Environment)
			}
		}
	}
	for _, t := range []*meta.ModsToml{j.NeoToml, j.ForgeToml} {
		if t != nil {
			for _, c := range t.Mixins {
				add(c, "*")
			}
		}
	}
	for _, c := range strings.Split(j.Manifest["MixinConfigs"], ",") {
		add(strings.TrimSpace(c), "*")
	}
	// Configs that are not referenced anywhere (some loaders discover them by name).
	var extra []string
	for name := range files {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, ".json") && strings.Contains(strings.ToLower(name), "mixin") {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		add(name, "*")
	}
	return refs
}

func readEntry(f *zip.File) ([]byte, error) { return readLimited(f, maxEntrySize) }

func readLimited(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("entry too large (%d bytes)", f.UncompressedSize64)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, limit))
}

func (a *AssetStats) count(name string) {
	parts := strings.SplitN(name, "/", 4)
	if len(parts) < 4 {
		if path.Base(name) == "sounds.json" {
			a.Sounds++
		}
		return
	}
	own := parts[1] != "minecraft"
	switch parts[2] {
	case "textures":
		a.Textures++
	case "models":
		a.Models++
		if own && (strings.HasPrefix(parts[3], "item/") || strings.HasPrefix(parts[3], "items/")) {
			a.OwnItemModels++
		}
	case "items":
		if own {
			a.OwnItemModels++
		}
	case "blockstates":
		a.Blockstates++
		if own {
			a.OwnBlockstates++
		}
	case "sounds":
		a.Sounds++
	case "shaders":
		a.Shaders++
	case "lang":
		a.Lang++
	}
}

func (a *AssetStats) countData(name string) {
	a.Data++
	parts := strings.SplitN(name, "/", 4)
	if len(parts) < 4 {
		return
	}
	switch parts[2] {
	case "worldgen", "structures", "structure":
		a.Worldgen++
	case "recipe", "recipes", "loot_table", "loot_tables":
		if parts[1] != "minecraft" {
			a.OwnRecipesLoot++
		}
	}
}
