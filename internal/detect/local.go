package detect

import (
	"fmt"
	"slices"
	"strings"

	"github.com/xycld/minecraft-mod-filter/internal/inspect"
	"github.com/xycld/minecraft-mod-filter/internal/meta"
	"github.com/xycld/minecraft-mod-filter/internal/side"
)

const (
	req   = side.Required
	opt   = side.Optional
	unsup = side.Unsupported
	unk   = side.Unknown
)

func ev(rule string, client, server side.Need, weight int, args ...any) Evidence {
	e := Evidence{Source: "jar", Rule: rule, Client: client, Server: server, Weight: weight}
	for _, a := range args {
		e.Args = append(e.Args, fmt.Sprint(a))
	}
	return e
}

// Local derives evidence from the jar alone. target restricts loader-specific
// metadata to one loader for multi-loader jars; "" considers all of them.
func Local(j *inspect.Jar, target Loader) []Evidence {
	var out []Evidence
	uses := MetadataFilter(j, target)

	if uses(Fabric) && j.Fabric != nil {
		out = append(out, fabricRules(j.Fabric)...)
	}
	if uses(Quilt) && j.Quilt != nil {
		out = append(out, fabricRules(j.Quilt)...)
	}
	if uses(NeoForge) && j.NeoToml != nil {
		out = append(out, tomlRules(j.NeoToml)...)
	}
	if uses(Forge) && j.ForgeToml != nil {
		out = append(out, tomlRules(j.ForgeToml)...)
	}
	out = append(out, annotationRules(j)...)
	out = append(out, mixinRules(j)...)
	out = append(out, bytecodeRules(j)...)
	out = append(out, nestedRules(j, target)...)
	out = append(out, networkRules(j, out)...)
	// Never wins against a real claim; keeps a side from staying unknown.
	return append(out, ev("fallback.both", req, req, Fallback))
}

func fabricRules(m *meta.FabricMod) []Evidence {
	file := "fabric.mod.json"
	if m.Quilt {
		file = "quilt.mod.json"
	}
	var out []Evidence
	switch m.Environment {
	case "client":
		out = append(out, ev("meta.environment.client", req, unsup, Hard, file))
	case "server":
		out = append(out, ev("meta.environment.server", unsup, req, Hard, file))
	}
	_, hasMain := m.Entrypoints["main"]
	_, hasClient := m.Entrypoints["client"]
	_, hasServer := m.Entrypoints["server"]
	switch {
	case hasClient && !hasMain && !hasServer:
		out = append(out, ev("meta.entrypoints.client", req, unsup, Strong, file))
	case hasServer && !hasMain && !hasClient:
		out = append(out, ev("meta.entrypoints.server", opt, req, Strong, file))
	}
	return out
}

func tomlRules(t *meta.ModsToml) []Evidence {
	file := "META-INF/mods.toml"
	if t.NeoForge {
		file = "META-INF/neoforge.mods.toml"
	}
	var out []Evidence
	if t.ClientSideOnly {
		out = append(out, ev("meta.clientSideOnly", req, unsup, Hard, file))
	}
	for _, m := range t.Mods {
		switch m.DisplayTest {
		case "IGNORE_SERVER_VERSION":
			out = append(out, ev("meta.displayTest.ignoreServer", opt, req, Strong, file, m.ModID))
		case "IGNORE_ALL_VERSION":
			out = append(out, ev("meta.displayTest.ignoreAll", opt, opt, Weak, file, m.ModID))
		}
	}
	// The side of the dependency on the game or loader itself tells where the
	// mod expects to run.
	var sides []string
	for _, list := range t.Dependencies {
		for _, d := range list {
			if platformIDs[strings.ToLower(d.ModID)] && d.Side != "" {
				sides = append(sides, d.Side)
			}
		}
	}
	if len(sides) > 0 {
		switch {
		case all(sides, "CLIENT"):
			out = append(out, ev("meta.depSide.client", req, unsup, Strong, file))
		case all(sides, "SERVER"):
			out = append(out, ev("meta.depSide.server", opt, req, Strong, file))
		}
	}
	return out
}

func all(list []string, v string) bool {
	for _, s := range list {
		if s != v {
			return false
		}
	}
	return true
}

func annotationRules(j *inspect.Jar) []Evidence {
	mods := j.Classes.ModAnnotations
	if len(mods) == 0 {
		return nil
	}
	var out []Evidence
	clientOnly, serverOnly, anyRemote := true, true, false
	for _, m := range mods {
		switch {
		case m.ClientSideOnly || slices.Equal(m.Dist, []string{"CLIENT"}):
			serverOnly = false
		case m.ServerSideOnly || slices.Equal(m.Dist, []string{"DEDICATED_SERVER"}):
			clientOnly = false
		default:
			clientOnly, serverOnly = false, false
		}
		if m.AcceptableRemoteVersions == "*" {
			anyRemote = true
		}
	}
	switch {
	case clientOnly:
		out = append(out, ev("class.mod.client", req, unsup, Hard, mods[0].Class))
	case serverOnly:
		out = append(out, ev("class.mod.server", unsup, req, Hard, mods[0].Class))
	}
	if anyRemote {
		out = append(out, ev("class.mod.anyRemote", opt, opt, Weak))
	}
	return out
}

func mixinRules(j *inspect.Jar) []Evidence {
	client, common, server := 0, 0, 0
	for _, m := range j.Mixins {
		switch m.Environment {
		case "client":
			client += m.Common + m.Client
		case "server":
			server += m.Common + m.Server
		default:
			client += m.Client + m.CommonOnClient
			common += m.Common - m.CommonOnClient
			server += m.Server
		}
	}
	switch {
	case client > 0 && common == 0 && server == 0:
		return []Evidence{ev("mixin.clientOnly", req, unsup, Strong, client)}
	case server > 0 && common == 0 && client == 0:
		return []Evidence{ev("mixin.serverOnly", opt, req, Strong, server)}
	case common > 0:
		// Mixins into shared game code change behaviour wherever the game logic runs.
		return []Evidence{ev("mixin.common", unk, req, Weak+10, common)}
	}
	return nil
}

func bytecodeRules(j *inspect.Jar) []Evidence {
	c, a := j.Classes, j.Assets
	var out []Evidence
	if c.Total == 0 {
		if len(j.Nested) > 0 {
			return nil // a container; judged by its contents
		}
		switch {
		case a.Data > 0 && !a.ClientResources():
			out = append(out, ev("res.dataOnly", opt, req, Strong, a.Data))
		case a.ClientResources() && a.Data == 0:
			out = append(out, ev("res.assetsOnly", req, opt, Moderate))
		case a.ClientResources() && a.Data > 0:
			out = append(out, ev("res.both", req, req, Moderate))
		}
		return out
	}
	if a.AddsContent() {
		// New blocks/items must be registered on both sides.
		out = append(out, ev("res.content", req, req, Strong+4, a.OwnBlockstates, a.OwnItemModels))
	} else if c.Registers && a.Models > 0 {
		out = append(out, ev("class.registersContent", req, req, Moderate))
	}
	if a.Worldgen > 0 {
		out = append(out, ev("res.worldgen", unk, req, Moderate, a.Worldgen))
	}
	if c.Game == 0 {
		return out
	}
	ratio := float64(c.Client) / float64(c.Game)
	switch {
	case c.Client == 0 && !a.ClientResources():
		out = append(out, ev("class.noClient", opt, req, Moderate, c.Game))
	case c.Client == c.Game:
		out = append(out, ev("class.allClient", req, unsup, Moderate, c.Game))
	case ratio >= 0.8 && !c.Registers:
		out = append(out, ev("class.mostlyClient", req, unsup, Weak, c.Client, c.Game))
	}
	if c.Client > 0 {
		out = append(out, ev("class.hasClient", req, unk, Moderate, c.Client))
	}
	if ratio < 0.8 {
		out = append(out, ev("class.commonLogic", unk, req, Weak+10, c.Game-c.Client))
	}
	return out
}

// networkRules: a mod that registers network payloads has a server-side
// counterpart, so client-only claims below Hard are softened to "server
// optional"; otherwise it is weak evidence that the server needs the mod.
func networkRules(j *inspect.Jar, evs []Evidence) []Evidence {
	if !j.Classes.Networking {
		return nil
	}
	strongest := 0
	for _, e := range evs {
		if e.Server == unsup && e.Weight < Hard {
			strongest = max(strongest, e.Weight)
		}
	}
	if strongest > 0 {
		return []Evidence{ev("class.networking.optional", unk, opt, strongest+2)}
	}
	return []Evidence{ev("class.networking", unk, req, Weak+10)}
}

// nestedRules judges a jar with no code of its own by what it bundles.
func nestedRules(j *inspect.Jar, target Loader) []Evidence {
	if len(j.Nested) == 0 || j.Classes.Game > 0 || len(j.Mixins) > 0 {
		return nil
	}
	var envs []side.Env
	for _, n := range j.Nested {
		v := Resolve(Local(n, target))
		if v.Confidence != Low {
			envs = append(envs, v.Env)
		}
	}
	if len(envs) == 0 {
		return nil
	}
	combined := envs[0]
	for _, e := range envs[1:] {
		combined.Client = max(combined.Client, e.Client)
		combined.Server = max(combined.Server, e.Server)
	}
	return []Evidence{ev("nested.combined", combined.Client, combined.Server, Moderate, len(envs))}
}
