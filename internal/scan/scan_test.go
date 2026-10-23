package scan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xycld/minecraft-mod-filter/internal/pack"
	"github.com/xycld/minecraft-mod-filter/internal/provider/modrinth"
	"github.com/xycld/minecraft-mod-filter/internal/side"
	"github.com/xycld/minecraft-mod-filter/internal/testjar"
)

func neoMod(id string, deps ...string) *testjar.Jar {
	toml := "modLoader=\"javafml\"\n[[mods]]\nmodId=\"" + id + "\"\ndisplayName=\"" + id + "\"\n"
	for _, d := range deps {
		toml += "[[dependencies." + id + "]]\nmodId=\"" + d + "\"\ntype=\"required\"\nside=\"BOTH\"\n"
	}
	return testjar.New().File("META-INF/neoforge.mods.toml", toml)
}

func clientOnly(j *testjar.Jar, id string) *testjar.Jar {
	return j.Class(testjar.Class{Name: id + "/Main", Refs: []string{"net/minecraft/world/level/Level"},
		Annotations: []testjar.Annotation{{Type: "net/neoforged/fml/common/Mod", Values: map[string]any{
			"value": id, "dist": []any{testjar.Enum{Type: "net/neoforged/api/distmarker/Dist", Name: "CLIENT"}},
		}}}})
}

func library(j *testjar.Jar, id string) *testjar.Jar {
	// Shared code with a client part and no content: on its own, "both" at
	// low weight, so its placement is decided by what depends on it.
	return j.Class(testjar.Class{Name: id + "/Api", Refs: []string{"net/minecraft/world/level/Level"}}).
		Class(testjar.Class{Name: id + "/client/Screen", Refs: []string{"net/minecraft/client/gui/screens/Screen"}})
}

func content(j *testjar.Jar, id string) *testjar.Jar {
	return j.Class(testjar.Class{Name: id + "/Blocks", Refs: []string{"net/minecraft/world/level/Level"}}).
		File("assets/"+id+"/blockstates/thing.json", "{}")
}

func bytesLoader(b []byte) func() ([]byte, error) {
	return func() ([]byte, error) { return b, nil }
}

func run(t *testing.T, items map[string][]byte, opts Options) map[string]*Mod {
	t.Helper()
	pk := &pack.Pack{Format: "directory", Loader: "neoforge"}
	for name, data := range items {
		pk.Items = append(pk.Items, pack.Item{Path: name, Load: bytesLoader(data)})
	}
	res, err := Run(context.Background(), pk, opts)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*Mod{}
	for _, m := range res.Mods {
		out[m.File] = m
	}
	return out
}

func TestLibraryNeededByServerMod(t *testing.T) {
	mods := run(t, map[string][]byte{
		"content.jar": content(neoMod("content", "lib"), "content").Bytes(),
		"lib.jar":     library(neoMod("lib"), "lib").Bytes(),
	}, Options{})
	lib := mods["lib.jar"]
	if lib.Verdict.Env.Server != side.Required || lib.Verdict.Env.Client != side.Required {
		t.Errorf("lib = %s, want both: %+v", lib.Verdict.Category, lib.Verdict.Evidence)
	}
	if len(lib.RequiredBy) != 1 || lib.RequiredBy[0] != "content" {
		t.Errorf("RequiredBy = %v", lib.RequiredBy)
	}
}

func TestLibraryChainOnlyUsedByClientMods(t *testing.T) {
	// ui (client-only) -> octolib -> architectury: neither library is needed
	// on a server, which is what real modpack authors do.
	mods := run(t, map[string][]byte{
		"ui.jar":           clientOnly(neoMod("ui", "octolib"), "ui").Bytes(),
		"octolib.jar":      library(neoMod("octolib", "architectury"), "octolib").Bytes(),
		"architectury.jar": library(neoMod("architectury"), "architectury").Bytes(),
	}, Options{})
	for _, f := range []string{"octolib.jar", "architectury.jar"} {
		if s := mods[f].Verdict.Env.Server; s != side.Optional {
			t.Errorf("%s server = %s, want optional; evidence %+v", f, s, mods[f].Verdict.Evidence)
		}
	}
	if s := mods["ui.jar"].Verdict.Env.Server; s != side.Unsupported {
		t.Errorf("ui server = %s", s)
	}
}

func TestDependencyIDsMatchAcrossLoaders(t *testing.T) {
	// A Fabric mod (run through Connector) depends on "fabric-api"; the
	// NeoForge port registers "fabric_api".
	mods := run(t, map[string][]byte{
		"fabricmod.jar": content(testjar.New().File("fabric.mod.json", `{"id":"fabricmod","depends":{"fabric-api":"*"}}`), "fabricmod").Bytes(),
		"ffapi.jar":     neoMod("fabric_api").Bytes(),
	}, Options{})
	if got := mods["ffapi.jar"].RequiredBy; len(got) != 1 {
		t.Errorf("fabric_api RequiredBy = %v, want the fabric mod", got)
	}
}

func TestMissingDependencies(t *testing.T) {
	pk := &pack.Pack{Loader: "neoforge", Items: []pack.Item{{Path: "a.jar", Load: bytesLoader(neoMod("a", "nothere", "minecraft").Bytes())}}}
	res, err := Run(context.Background(), pk, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Missing) != 1 || res.Missing[0] != "nothere" {
		t.Errorf("missing = %v", res.Missing)
	}
}

func TestModrinthEvidence(t *testing.T) {
	data := neoMod("perf").Class(testjar.Class{Name: "perf/Main", Refs: []string{"net/minecraft/world/level/Level"}}).Bytes()
	sha := sha1Hex(data)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/version_files":
			var body struct{ Hashes []string }
			json.NewDecoder(r.Body).Decode(&body)
			if len(body.Hashes) != 1 || body.Hashes[0] != sha {
				t.Errorf("hashes = %v", body.Hashes)
			}
			json.NewEncoder(w).Encode(map[string]any{sha: map[string]any{
				"id": "v1", "project_id": "p1", "environment": "client_only", "version_number": "1.0",
			}})
		case strings.HasPrefix(r.URL.Path, "/projects"):
			json.NewEncoder(w).Encode([]map[string]any{{"id": "p1", "slug": "perf", "title": "Perf", "project_type": "mod"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	mr := modrinth.New("test")
	mr.BaseURL = srv.URL

	mods := run(t, map[string][]byte{"perf.jar": data}, Options{Modrinth: mr})
	m := mods["perf.jar"]
	if m.Modrinth == nil || m.Modrinth.Slug != "perf" || !m.Modrinth.ByHash {
		t.Fatalf("modrinth ref = %+v", m.Modrinth)
	}
	// The author's explicit client_only beats the local "no client code" heuristic.
	if m.Verdict.Category != "client-only" {
		t.Errorf("category = %s, evidence %+v", m.Verdict.Category, m.Verdict.Evidence)
	}
}

func clientMixins(j *testjar.Jar, id string) *testjar.Jar {
	return j.File(id+".mixins.json", `{"package":"`+id+`.mixin","client":["Hud"]}`).
		Class(testjar.Class{Name: id + "/mixin/Hud", Annotations: []testjar.Annotation{{
			Type: "org/spongepowered/asm/mixin/Mixin", Values: map[string]any{"value": []any{testjar.ClassRef("net/minecraft/client/gui/Gui")}},
		}}})
}

// Regression: a library already required when first visited must still be
// re-required after the orphan rule lowered it (review finding H1).
func TestPropagationOrderIndependent(t *testing.T) {
	jars := map[string][]byte{
		"m.jar": library(neoMod("m", "p"), "m").Bytes(),
		"p.jar": library(neoMod("p"), "p").Bytes(),
		"x.jar": clientMixins(neoMod("x", "m"), "x").Bytes(),
		"y.jar": clientMixins(neoMod("y", "x"), "y").Bytes(),
		"r.jar": content(neoMod("r", "y"), "r").Bytes(),
	}
	for _, order := range [][]string{
		{"m.jar", "p.jar", "x.jar", "y.jar", "r.jar"},
		{"r.jar", "y.jar", "x.jar", "m.jar", "p.jar"},
	} {
		pk := &pack.Pack{Loader: "neoforge"}
		for _, f := range order {
			pk.Items = append(pk.Items, pack.Item{Path: f, Load: bytesLoader(jars[f])})
		}
		res, err := Run(context.Background(), pk, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range res.Mods {
			if m.Verdict.Env.Server != side.Required {
				t.Errorf("order %v: %s server = %s, want required (a server mod needs the whole chain)", order, m.File, m.Verdict.Env.Server)
			}
		}
	}
}

// Regression: bundling a library must not make a client-only mod required on
// the server when a standalone copy exists (review finding H6).
func TestBundlerNotRequiredForBundledLibrary(t *testing.T) {
	cloth := library(neoMod("cloth_config"), "cloth")
	hud := clientMixins(neoMod("hud"), "hud").Raw("META-INF/jarjar/cloth.jar", cloth.Bytes())
	mods := run(t, map[string][]byte{
		"hud.jar":   hud.Bytes(),
		"cloth.jar": cloth.Bytes(),
		"srv.jar":   content(neoMod("srv", "cloth_config"), "srv").Bytes(),
	}, Options{})
	if s := mods["hud.jar"].Verdict.Env.Server; s != side.Unsupported {
		t.Errorf("hud server = %s, want unsupported; evidence %+v", s, mods["hud.jar"].Verdict.Evidence)
	}
	if s := mods["cloth.jar"].Verdict.Env.Server; s != side.Required {
		t.Errorf("cloth server = %s, want required", s)
	}
}

// Regression: one multi-loader jar must not flip a Fabric folder to
// NeoForge (review finding H3).
func TestInferLoaderByMajority(t *testing.T) {
	fabric := func(id string) []byte {
		return testjar.New().File("fabric.mod.json", `{"id":"`+id+`","environment":"client"}`).Bytes()
	}
	pk := &pack.Pack{Items: []pack.Item{
		{Path: "a.jar", Load: bytesLoader(fabric("a"))},
		{Path: "b.jar", Load: bytesLoader(fabric("b"))},
		{Path: "c.jar", Load: bytesLoader(fabric("c"))},
		{Path: "multi.jar", Load: bytesLoader(neoMod("multi").File("fabric.mod.json", `{"id":"multi"}`).Bytes())},
	}}
	res, err := Run(context.Background(), pk, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Loader != "fabric" {
		t.Errorf("loader = %q, want fabric", res.Loader)
	}
	for _, m := range res.Mods {
		if m.File == "a.jar" && m.Verdict.Category != "client-only" {
			t.Errorf("a.jar = %s, want client-only", m.Verdict.Category)
		}
	}
}
