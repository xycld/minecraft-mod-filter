package inspect

import (
	"testing"

	"github.com/xycld/minecraft-mod-filter/internal/testjar"
)

func TestCurseForgeFingerprint(t *testing.T) {
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	// Reference values from an independent implementation of MurmurHash2.
	for _, tc := range []struct {
		in   []byte
		want uint32
	}{
		{[]byte(""), 1540447798},
		{[]byte("a"), 626045324},
		{[]byte("hello world"), 2824650221},
		{[]byte("The quick brown fox\njumps over\tthe lazy dog\r\n"), 3751777527},
		{all, 2094645347},
	} {
		if got := CurseForgeFingerprint(tc.in); got != tc.want {
			t.Errorf("fingerprint(%q) = %d, want %d", tc.in[:min(len(tc.in), 20)], got, tc.want)
		}
	}
}

func TestOpenCollectsFacts(t *testing.T) {
	jar := testjar.New().
		File("META-INF/neoforge.mods.toml", `
modLoader="javafml"
[[mods]]
modId="demo"
displayName="Demo"
[[mixins]]
config="demo.mixins.json"
`).
		File("demo.mixins.json", `{"package":"com.demo.mixin","mixins":["LevelMixin","ScreenMixin"],"client":["HudMixin"]}`).
		Class(testjar.Class{Name: "com/demo/mixin/LevelMixin", Annotations: []testjar.Annotation{{
			Type: "org/spongepowered/asm/mixin/Mixin", Values: map[string]any{"value": []any{testjar.ClassRef("net/minecraft/world/level/Level")}},
		}}}).
		Class(testjar.Class{Name: "com/demo/mixin/ScreenMixin", Annotations: []testjar.Annotation{{
			Type: "org/spongepowered/asm/mixin/Mixin", Values: map[string]any{"targets": []any{"net.minecraft.client.gui.screens.Screen"}},
		}}}).
		Class(testjar.Class{Name: "com/demo/Demo", Refs: []string{"net/minecraft/world/item/Item", "net/neoforged/neoforge/registries/DeferredRegister"},
			Annotations: []testjar.Annotation{{Type: "net/neoforged/fml/common/Mod", Values: map[string]any{"value": "demo"}}}}).
		Class(testjar.Class{Name: "com/demo/render/Renderer", Refs: []string{"net/minecraft/client/renderer/RenderType"}}).
		Class(testjar.Class{Name: "com/demo/client/Keys", Refs: []string{"net/minecraft/world/entity/player/Player"}}).
		File("assets/demo/blockstates/thing.json", "{}").
		File("assets/demo/textures/block/thing.png", "").
		File("assets/minecraft/blockstates/stone.json", "{}").
		File("data/demo/worldgen/biome/x.json", "{}").
		File("data/demo/recipe/thing.json", "{}").
		Raw("META-INF/jarjar/lib.jar", testjar.New().File("fabric.mod.json", `{"id":"lib","environment":"client"}`).Bytes()).
		Bytes()

	j := Open("demo.jar", jar)
	if len(j.Problems) > 0 {
		t.Fatalf("problems: %v", j.Problems)
	}
	if j.NeoToml == nil || j.NeoToml.Mods[0].ModID != "demo" {
		t.Fatalf("neoforge.mods.toml not parsed: %+v", j.NeoToml)
	}
	c := j.Classes
	if c.Total != 5 || c.MixinClasses != 2 || c.Game != 3 || c.Client != 2 {
		t.Errorf("class stats = %+v, want total 5, mixins 2, game 3, client 2 (renderer by ref, keys by package)", c)
	}
	if !c.Registers || len(c.ModAnnotations) != 1 || c.ModAnnotations[0].ModID != "demo" {
		t.Errorf("registers/@Mod = %v/%+v", c.Registers, c.ModAnnotations)
	}
	if len(j.Mixins) != 1 {
		t.Fatalf("mixins = %+v", j.Mixins)
	}
	if mx := j.Mixins[0]; mx.Common != 2 || mx.Client != 1 || mx.CommonOnClient != 1 {
		t.Errorf("mixin = %+v, want 2 common (1 targeting the client), 1 client", mx)
	}
	a := j.Assets
	if a.OwnBlockstates != 1 || a.Blockstates != 2 || a.Worldgen != 1 || a.OwnRecipesLoot != 1 || !a.AddsContent() {
		t.Errorf("assets = %+v", a)
	}
	if len(j.Nested) != 1 || j.Nested[0].Fabric == nil || j.Nested[0].Fabric.Environment != "client" {
		t.Errorf("nested = %+v", j.Nested)
	}
	if len(j.SHA1) != 40 || len(j.SHA512) != 128 {
		t.Errorf("hashes not computed")
	}
}

func TestOpenNotAZip(t *testing.T) {
	j := Open("broken.jar", []byte("definitely not a zip"))
	if len(j.Problems) == 0 {
		t.Error("expected a problem for a non-zip file")
	}
}
