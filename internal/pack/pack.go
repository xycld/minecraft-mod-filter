// Package pack loads the things a user can point the scanner at: a mods
// directory, a single jar, a Modrinth .mrpack or a CurseForge modpack zip.
package pack

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xycld/minecraft-mod-filter/internal/side"
)

const maxJarSize = 512 << 20

// Item is one mod in a pack. Load is nil when the pack only references the
// file (mrpack/CurseForge manifest entries); the scanner may download it.
// Jars are read lazily so a large pack is never held in memory at once.
type Item struct {
	Path     string
	Load     func() ([]byte, error)
	SHA1     string
	SHA512   string
	Size     int64
	URLs     []string  // download locations from the pack index
	Declared *side.Env // environment declared by the pack author

	CurseForgeProject int
	CurseForgeFile    int
}

// Pack is a loaded set of mods.
type Pack struct {
	Name        string
	Version     string
	Format      string // "directory", "jar", "mrpack", "curseforge"
	Loader      string // "fabric", "forge", "neoforge", "quilt" or ""
	GameVersion string
	Items       []Item
	Warnings    []string

	closer io.Closer // the pack archive, kept open for lazy override reads
}

// Close releases the pack archive, if any.
func (p *Pack) Close() error {
	if p.closer != nil {
		return p.closer.Close()
	}
	return nil
}

func fileLoader(p string) func() ([]byte, error) {
	return func() ([]byte, error) { return readFile(p) }
}

// Load detects the input kind from path and loads it.
func Load(p string) (*Pack, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return loadDir(p)
	}
	switch strings.ToLower(filepath.Ext(p)) {
	case ".jar":
		return &Pack{Name: filepath.Base(p), Format: "jar", Items: []Item{{Path: filepath.Base(p), Load: fileLoader(p), Size: st.Size()}}}, nil
	case ".mrpack":
		return loadMrpack(p)
	case ".zip":
		return loadCurseForge(p)
	}
	return nil, fmt.Errorf("%s: unsupported input (expected a directory, .jar, .mrpack or .zip)", p)
}

func readFile(p string) ([]byte, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxJarSize {
		return nil, fmt.Errorf("%s: file too large (%d bytes)", p, st.Size())
	}
	return os.ReadFile(p)
}

// loadDir loads *.jar files from a mods directory. If dir is an instance
// root containing mods/, that subdirectory is used.
func loadDir(dir string) (*Pack, error) {
	if st, err := os.Stat(filepath.Join(dir, "mods")); err == nil && st.IsDir() {
		dir = filepath.Join(dir, "mods")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	pk := &Pack{Name: filepath.Base(filepath.Dir(dir + "/")), Format: "directory"}
	if abs, err := filepath.Abs(dir); err == nil {
		pk.Name = filepath.Base(abs)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".jar") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			pk.Warnings = append(pk.Warnings, err.Error())
			continue
		}
		pk.Items = append(pk.Items, Item{Path: e.Name(), Load: fileLoader(filepath.Join(dir, e.Name())), Size: info.Size()})
	}
	if len(pk.Items) == 0 {
		return nil, fmt.Errorf("%s: no .jar files found", dir)
	}
	return pk, nil
}

func loadMrpack(p string) (*Pack, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, err
	}
	var index struct {
		Name         string            `json:"name"`
		VersionID    string            `json:"versionId"`
		Dependencies map[string]string `json:"dependencies"`
		Files        []struct {
			Path   string            `json:"path"`
			Hashes map[string]string `json:"hashes"`
			Env    *struct {
				Client string `json:"client"`
				Server string `json:"server"`
			} `json:"env"`
			Downloads []string `json:"downloads"`
			FileSize  int64    `json:"fileSize"`
		} `json:"files"`
	}
	if err := readZipJSON(&zr.Reader, "modrinth.index.json", &index); err != nil {
		zr.Close()
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	pk := &Pack{Name: index.Name, Version: index.VersionID, Format: "mrpack", closer: zr}
	pk.GameVersion = index.Dependencies["minecraft"]
	for _, l := range []struct{ key, loader string }{
		{"neoforge", "neoforge"}, {"forge", "forge"}, {"quilt-loader", "quilt"}, {"fabric-loader", "fabric"},
	} {
		if _, ok := index.Dependencies[l.key]; ok {
			pk.Loader = l.loader
			break
		}
	}
	for _, f := range index.Files {
		if !isModPath(f.Path) {
			continue
		}
		it := Item{Path: path.Base(f.Path), SHA1: f.Hashes["sha1"], SHA512: f.Hashes["sha512"], Size: f.FileSize, URLs: f.Downloads}
		if f.Env != nil {
			c, err1 := side.ParseNeed(f.Env.Client)
			s, err2 := side.ParseNeed(f.Env.Server)
			if err1 == nil && err2 == nil {
				it.Declared = &side.Env{Client: c, Server: s}
			}
		}
		pk.Items = append(pk.Items, it)
	}
	pk.Items = append(pk.Items, overrideJars(&zr.Reader, "overrides", "server-overrides", "client-overrides")...)
	return pk, nil
}

func loadCurseForge(p string) (*Pack, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Minecraft struct {
			Version    string `json:"version"`
			ModLoaders []struct {
				ID      string `json:"id"`
				Primary bool   `json:"primary"`
			} `json:"modLoaders"`
		} `json:"minecraft"`
		Files []struct {
			ProjectID int  `json:"projectID"`
			FileID    int  `json:"fileID"`
			Required  bool `json:"required"`
		} `json:"files"`
		Overrides string `json:"overrides"`
	}
	if err := readZipJSON(&zr.Reader, "manifest.json", &manifest); err != nil {
		zr.Close()
		return nil, fmt.Errorf("%s: not a CurseForge modpack: %w", p, err)
	}
	pk := &Pack{Name: manifest.Name, Version: manifest.Version, Format: "curseforge", GameVersion: manifest.Minecraft.Version, closer: zr}
	for _, l := range manifest.Minecraft.ModLoaders {
		if l.Primary || pk.Loader == "" {
			switch name, _, _ := strings.Cut(l.ID, "-"); name {
			case "forge", "neoforge", "fabric", "quilt":
				pk.Loader = name
			}
		}
	}
	for _, f := range manifest.Files {
		pk.Items = append(pk.Items, Item{
			Path:              fmt.Sprintf("curseforge:%d/%d", f.ProjectID, f.FileID),
			CurseForgeProject: f.ProjectID,
			CurseForgeFile:    f.FileID,
		})
	}
	overrides := manifest.Overrides
	if overrides == "" {
		overrides = "overrides"
	}
	pk.Items = append(pk.Items, overrideJars(&zr.Reader, overrides)...)
	return pk, nil
}

func isModPath(p string) bool {
	return strings.HasPrefix(p, "mods/") && strings.EqualFold(path.Ext(p), ".jar") && !strings.Contains(p[5:], "/")
}

// overrideJars reads mod jars shipped inside the pack's override folders.
func overrideJars(zr *zip.Reader, roots ...string) []Item {
	var items []Item
	for _, f := range zr.File {
		for _, root := range roots {
			rel, ok := strings.CutPrefix(f.Name, root+"/")
			if !ok || !isModPath(rel) {
				continue
			}
			it := Item{Path: path.Base(rel), Load: func() ([]byte, error) { return readZipFile(f) }, Size: int64(f.UncompressedSize64)}
			switch root {
			case "server-overrides":
				it.Declared = &side.Env{Client: side.Unsupported, Server: side.Required}
			case "client-overrides":
				it.Declared = &side.Env{Client: side.Required, Server: side.Unsupported}
			}
			items = append(items, it)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items
}

func readZipJSON(zr *zip.Reader, name string, v any) error {
	for _, f := range zr.File {
		if f.Name == name {
			data, err := readZipFile(f)
			if err != nil {
				return err
			}
			return json.Unmarshal(data, v)
		}
	}
	return errors.New(name + " not found")
}

func readZipFile(f *zip.File) ([]byte, error) {
	if f.UncompressedSize64 > maxJarSize {
		return nil, fmt.Errorf("entry too large")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxJarSize))
}
