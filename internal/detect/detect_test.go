package detect

import (
	"testing"

	"github.com/xycld/minecraft-mod-filter/internal/inspect"
	"github.com/xycld/minecraft-mod-filter/internal/side"
	"github.com/xycld/minecraft-mod-filter/internal/testjar"
)

const (
	clientRef = "net/minecraft/client/Minecraft"
	levelRef  = "net/minecraft/world/level/Level"
)

func mixinClass(name string, target string) testjar.Class {
	return testjar.Class{Name: name, Annotations: []testjar.Annotation{{
		Type: "org/spongepowered/asm/mixin/Mixin", Values: map[string]any{"value": []any{testjar.ClassRef(target)}},
	}}}
}

func neoToml(extra string) string {
	return "modLoader=\"javafml\"\n[[mods]]\nmodId=\"demo\"\n" + extra
}

func TestLocalRules(t *testing.T) {
	tests := []struct {
		name   string
		jar    *testjar.Jar
		target Loader
		want   string
		conf   Confidence
	}{
		{
			name: "fabric environment client",
			jar:  testjar.New().File("fabric.mod.json", `{"id":"a","environment":"client"}`),
			want: "client-only", conf: High,
		},
		{
			name: "fabric environment server",
			jar:  testjar.New().File("fabric.mod.json", `{"id":"a","environment":"server"}`),
			want: "server-only", conf: High,
		},
		{
			name: "fabric only client entrypoints",
			jar:  testjar.New().File("fabric.mod.json", `{"id":"a","entrypoints":{"client":["a.C"],"modmenu":["a.M"]}}`),
			want: "client-only", conf: High,
		},
		{
			name: "neoforge @Mod(dist=CLIENT)",
			jar: testjar.New().File("META-INF/neoforge.mods.toml", neoToml("")).Class(testjar.Class{
				Name: "a/A", Refs: []string{levelRef},
				Annotations: []testjar.Annotation{{Type: "net/neoforged/fml/common/Mod", Values: map[string]any{
					"value": "demo", "dist": []any{testjar.Enum{Type: "net/neoforged/api/distmarker/Dist", Name: "CLIENT"}},
				}}},
			}),
			want: "client-only", conf: High,
		},
		{
			name: "forge 1.12 clientSideOnly",
			jar: testjar.New().File("mcmod.info", `[{"modid":"old"}]`).Class(testjar.Class{
				Name: "a/Old", Refs: []string{levelRef},
				Annotations: []testjar.Annotation{{Type: "net/minecraftforge/fml/common/Mod", Values: map[string]any{"modid": "old", "clientSideOnly": true}}},
			}),
			want: "client-only", conf: High,
		},
		{
			name: "dependency on the loader is client side",
			jar: testjar.New().File("META-INF/neoforge.mods.toml", neoToml(
				"[[dependencies.demo]]\nmodId=\"neoforge\"\ntype=\"required\"\nside=\"CLIENT\"\n[[dependencies.demo]]\nmodId=\"minecraft\"\ntype=\"required\"\nside=\"CLIENT\"\n")),
			want: "client-only", conf: High,
		},
		{
			name: "displayTest IGNORE_SERVER_VERSION",
			jar:  testjar.New().File("META-INF/neoforge.mods.toml", neoToml("displayTest=\"IGNORE_SERVER_VERSION\"\n")),
			want: "server (client optional)", conf: High,
		},
		{
			name: "only client mixins",
			jar: testjar.New().
				File("META-INF/neoforge.mods.toml", neoToml("[[mixins]]\nconfig=\"a.mixins.json\"\n")).
				File("a.mixins.json", `{"package":"a.mixin","mixins":["Common"],"client":["Hud"]}`).
				Class(mixinClass("a/mixin/Common", "net/minecraft/client/gui/Gui")).
				Class(mixinClass("a/mixin/Hud", "net/minecraft/client/gui/Gui")),
			want: "client-only", conf: High,
		},
		{
			name: "data pack only",
			jar:  testjar.New().File("META-INF/mods.toml", neoToml("")).File("data/demo/worldgen/structure/x.json", "{}"),
			want: "server (client optional)", conf: High,
		},
		{
			name: "no client code",
			jar: testjar.New().File("META-INF/neoforge.mods.toml", neoToml("")).
				Class(testjar.Class{Name: "a/Tick", Refs: []string{levelRef, "net/minecraft/server/MinecraftServer"}}),
			want: "server (client optional)", conf: Medium,
		},
		{
			name: "content beats client mixins",
			jar: testjar.New().
				File("META-INF/neoforge.mods.toml", neoToml("[[mixins]]\nconfig=\"a.mixins.json\"\n")).
				File("a.mixins.json", `{"package":"a.mixin","client":["Hud"]}`).
				Class(mixinClass("a/mixin/Hud", "net/minecraft/client/gui/Gui")).
				Class(testjar.Class{Name: "a/Blocks", Refs: []string{levelRef}}).
				File("assets/demo/blockstates/chair.json", "{}"),
			want: "both", conf: Medium,
		},
		{
			name: "networking softens a client-only heuristic",
			jar: testjar.New().
				File("META-INF/neoforge.mods.toml", neoToml("[[mixins]]\nconfig=\"a.mixins.json\"\n")).
				File("a.mixins.json", `{"package":"a.mixin","client":["Hud"]}`).
				Class(mixinClass("a/mixin/Hud", "net/minecraft/client/gui/Gui")).
				Class(testjar.Class{Name: "a/Net", Refs: []string{"net/neoforged/neoforge/network/registration/PayloadRegistrar", clientRef}}),
			want: "client (server optional)", conf: Medium,
		},
		{
			name: "bundle of client-only jars",
			jar: testjar.New().
				Raw("META-INF/jarjar/a.jar", testjar.New().File("fabric.mod.json", `{"id":"a","environment":"client"}`).Bytes()).
				Raw("META-INF/jarjar/b.jar", testjar.New().File("fabric.mod.json", `{"id":"b","environment":"client"}`).Bytes()),
			want: "client-only", conf: Medium,
		},
		{
			name: "multi-loader jar uses the target loader's metadata",
			jar: testjar.New().
				File("fabric.mod.json", `{"id":"a","environment":"client"}`).
				File("META-INF/neoforge.mods.toml", neoToml("displayTest=\"IGNORE_SERVER_VERSION\"\n")),
			target: NeoForge,
			want:   "server (client optional)", conf: High,
		},
		{
			name:   "fabric mod in a quilt pack keeps its metadata",
			jar:    testjar.New().File("fabric.mod.json", `{"id":"a","environment":"client"}`),
			target: Quilt,
			want:   "client-only", conf: High,
		},
		{
			name: "nothing known",
			jar:  testjar.New().File("README.txt", "hi"),
			want: "both", conf: Low,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := Resolve(Local(inspect.Open("t.jar", tc.jar.Bytes()), tc.target))
			if v.Category != tc.want || v.Confidence != tc.conf {
				t.Errorf("got %s (%s), want %s (%s); evidence: %+v", v.Category, v.Confidence, tc.want, tc.conf, v.Evidence)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	e := func(c, s side.Need, w int) Evidence { return Evidence{Client: c, Server: s, Weight: w} }
	tests := []struct {
		name string
		evs  []Evidence
		want side.Env
		conf Confidence
	}{
		{"highest weight wins", []Evidence{e(side.Required, side.Required, Weak), e(side.Required, side.Unsupported, Hard)},
			side.Env{Client: side.Required, Server: side.Unsupported}, High},
		{"sides resolve independently", []Evidence{e(side.Optional, side.Unknown, Strong), e(side.Unknown, side.Required, Moderate)},
			side.Env{Client: side.Optional, Server: side.Required}, Medium},
		{"close conflict lowers confidence", []Evidence{e(side.Required, side.Unsupported, Strong), e(side.Unknown, side.Required, Strong-5)},
			side.Env{Client: side.Required, Server: side.Unsupported}, Medium},
		{"never unsupported on both sides", []Evidence{e(side.Unsupported, side.Unknown, Strong), e(side.Unknown, side.Unsupported, Moderate)},
			side.Env{Client: side.Unsupported, Server: side.Optional}, Medium},
		{"tie goes to the larger total", []Evidence{e(side.Required, side.Optional, Moderate), e(side.Required, side.Optional, Weak), e(side.Required, side.Required, Moderate)},
			side.Env{Client: side.Required, Server: side.Optional}, Medium},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := Resolve(tc.evs)
			if v.Env != tc.want || v.Confidence != tc.conf {
				t.Errorf("got %+v (%s), want %+v (%s)", v.Env, v.Confidence, tc.want, tc.conf)
			}
		})
	}
}

func TestDependenciesOfTargetLoader(t *testing.T) {
	j := inspect.Open("t.jar", testjar.New().
		File("fabric.mod.json", `{"id":"a","depends":{"fabric-api":"*","fabricloader":"*"}}`).
		File("META-INF/neoforge.mods.toml", neoToml("[[dependencies.demo]]\nmodId=\"architectury\"\ntype=\"required\"\n[[dependencies.demo]]\nmodId=\"neoforge\"\ntype=\"required\"\n")).
		Bytes())
	got := DependenciesOf(j, NeoForge)
	if len(got) != 1 || got[0].ModID != "architectury" || got[0].Side != "BOTH" {
		t.Errorf("neoforge deps = %+v", got)
	}
	got = DependenciesOf(j, Fabric)
	if len(got) != 1 || got[0].ModID != "fabric-api" {
		t.Errorf("fabric deps = %+v", got)
	}
}
