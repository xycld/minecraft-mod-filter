package meta

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// ModsToml is the subset of META-INF/mods.toml and neoforge.mods.toml that
// matters for side detection.
type ModsToml struct {
	NeoForge       bool
	ClientSideOnly bool
	Mods           []ForgeMod
	Dependencies   map[string][]ForgeDep // owner modId -> dependencies
	Mixins         []string
	Lenient        bool // parsed with the regex fallback because the TOML was invalid
}

type ForgeMod struct {
	ModID       string
	Version     string
	DisplayName string
	DisplayTest string
}

type ForgeDep struct {
	ModID     string
	Mandatory bool
	Side      string // "BOTH", "CLIENT", "SERVER" ("" when absent)
}

// ParseModsToml parses a Forge or NeoForge mods.toml file. Invalid TOML is
// common in the wild, so a line-based fallback recovers the important keys.
func ParseModsToml(data []byte, neoforge bool) (*ModsToml, error) {
	var raw struct {
		ClientSideOnly any `toml:"clientSideOnly"`
		Mods           []struct {
			ModID       string `toml:"modId"`
			Version     any    `toml:"version"`
			DisplayName string `toml:"displayName"`
			DisplayTest string `toml:"displayTest"`
		} `toml:"mods"`
		Dependencies map[string]any `toml:"dependencies"`
		Mixins       []struct {
			Config string `toml:"config"`
		} `toml:"mixins"`
	}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return parseModsTomlLenient(data, neoforge), nil
	}
	m := &ModsToml{NeoForge: neoforge, ClientSideOnly: isTrue(raw.ClientSideOnly), Dependencies: map[string][]ForgeDep{}}
	for _, mod := range raw.Mods {
		version, _ := mod.Version.(string)
		m.Mods = append(m.Mods, ForgeMod{ModID: mod.ModID, Version: version, DisplayName: mod.DisplayName, DisplayTest: mod.DisplayTest})
	}
	for owner, list := range raw.Dependencies {
		entries, _ := list.([]any)
		for _, e := range entries {
			fields, ok := e.(map[string]any)
			if !ok {
				continue
			}
			m.Dependencies[owner] = append(m.Dependencies[owner], forgeDep(fields))
		}
	}
	for _, mx := range raw.Mixins {
		m.Mixins = append(m.Mixins, mx.Config)
	}
	return m, nil
}

func forgeDep(f map[string]any) ForgeDep {
	d := ForgeDep{ModID: str(f["modId"]), Side: strings.ToUpper(str(f["side"]))}
	if t, ok := f["type"].(string); ok {
		d.Mandatory = strings.EqualFold(t, "required")
	} else {
		d.Mandatory = isTrue(f["mandatory"])
	}
	return d
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func isTrue(v any) bool {
	switch v := v.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	}
	return false
}

var (
	tomlKV      = regexp.MustCompile(`^\s*([A-Za-z]+)\s*=\s*"?([^"#]*)"?`)
	tomlDepHead = regexp.MustCompile(`^\s*\[\[\s*dependencies\.([^\]\s]+)\s*\]\]`)
)

func parseModsTomlLenient(data []byte, neoforge bool) *ModsToml {
	m := &ModsToml{NeoForge: neoforge, Dependencies: map[string][]ForgeDep{}, Lenient: true}
	var mod *ForgeMod
	var dep *ForgeDep
	depOwner := ""
	section := ""
	flush := func() {
		if mod != nil {
			m.Mods = append(m.Mods, *mod)
			mod = nil
		}
		if dep != nil {
			m.Dependencies[depOwner] = append(m.Dependencies[depOwner], *dep)
			dep = nil
		}
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "[[mods]]"):
			flush()
			mod, section = &ForgeMod{}, "mods"
			continue
		case tomlDepHead.MatchString(trim):
			flush()
			depOwner = tomlDepHead.FindStringSubmatch(trim)[1]
			dep, section = &ForgeDep{}, "dep"
			continue
		case strings.HasPrefix(trim, "[["):
			flush()
			section = strings.Trim(trim, "[] ")
			continue
		case strings.HasPrefix(trim, "["):
			flush()
			section = "table"
			continue
		}
		kv := tomlKV.FindStringSubmatch(line)
		if kv == nil {
			continue
		}
		key, val := kv[1], strings.TrimSpace(kv[2])
		switch section {
		case "":
			if key == "clientSideOnly" {
				m.ClientSideOnly = val == "true"
			}
		case "mods":
			switch key {
			case "modId":
				mod.ModID = val
			case "version":
				mod.Version = val
			case "displayName":
				mod.DisplayName = val
			case "displayTest":
				mod.DisplayTest = val
			}
		case "dep":
			switch key {
			case "modId":
				dep.ModID = val
			case "mandatory":
				dep.Mandatory = val == "true"
			case "type":
				dep.Mandatory = strings.EqualFold(val, "required")
			case "side":
				dep.Side = strings.ToUpper(val)
			}
		case "mixins":
			if key == "config" {
				m.Mixins = append(m.Mixins, val)
			}
		}
	}
	flush()
	return m
}

// McmodInfo is one entry of a legacy (Forge 1.12 and older) mcmod.info file.
type McmodInfo struct {
	ModID   string `json:"modid"`
	Name    string `json:"name"`
	Version string `json:"version"`
	MC      string `json:"mcversion"`
}

// ParseMcmodInfo parses both the bare-array and the {"modList": [...]} forms.
func ParseMcmodInfo(data []byte) ([]McmodInfo, error) {
	var list []McmodInfo
	if err := unmarshalLenient(data, &list); err == nil {
		return list, nil
	}
	var wrapped struct {
		ModList []McmodInfo `json:"modList"`
	}
	err := unmarshalLenient(data, &wrapped)
	return wrapped.ModList, err
}

// ParseManifest parses META-INF/MANIFEST.MF main attributes.
func ParseManifest(data []byte) map[string]string {
	attrs := map[string]string{}
	last := ""
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if line == "" {
			break // end of main section
		}
		if line[0] == ' ' && last != "" {
			attrs[last] += line[1:]
			continue
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			last = strings.TrimSpace(k)
			attrs[last] = strings.TrimSpace(v)
		}
	}
	return attrs
}
