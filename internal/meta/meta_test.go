package meta

import (
	"reflect"
	"testing"
)

func TestParseFabric(t *testing.T) {
	m, err := ParseFabric([]byte(`{
		"schemaVersion": 1,
		"id": "demo",
		"name": "Demo",
		"version": "1.0",
		"environment": "client",
		// hand-written files contain comments,
		"description": "a raw
newline",
		"entrypoints": {
			"client": ["com.demo.Client", {"adapter": "kotlin", "value": "com.demo.KtClient"}],
			"modmenu": ["com.demo.Menu"],
		},
		"mixins": ["demo.mixins.json", {"config": "demo.client.mixins.json", "environment": "client"}],
		"depends": {"fabricloader": ">=0.14", "fabric-api": "*", "cloth-config": ["^11", "^12"]},
		"provides": ["demo_alias"],
		"jars": [{"file": "META-INF/jars/lib.jar"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "demo" || m.Environment != "client" || m.Name != "Demo" {
		t.Errorf("basic fields: %+v", m)
	}
	if want := []string{"com.demo.Client", "com.demo.KtClient"}; !reflect.DeepEqual(m.Entrypoints["client"], want) {
		t.Errorf("client entrypoints = %v", m.Entrypoints["client"])
	}
	if want := []MixinRef{{"demo.mixins.json", "*"}, {"demo.client.mixins.json", "client"}}; !reflect.DeepEqual(m.Mixins, want) {
		t.Errorf("mixins = %v", m.Mixins)
	}
	if want := []string{"cloth-config", "fabric-api", "fabricloader"}; !reflect.DeepEqual(m.Depends, want) {
		t.Errorf("depends = %v", m.Depends)
	}
	if !reflect.DeepEqual(m.Jars, []string{"META-INF/jars/lib.jar"}) || !reflect.DeepEqual(m.Provides, []string{"demo_alias"}) {
		t.Errorf("jars/provides = %v/%v", m.Jars, m.Provides)
	}
}

func TestParseQuilt(t *testing.T) {
	m, err := ParseQuilt([]byte(`{
		"schema_version": 1,
		"quilt_loader": {
			"id": "qdemo", "version": "2.0", "metadata": {"name": "Q Demo"},
			"entrypoints": {"client_init": "com.q.Client", "init": ["com.q.Main"]},
			"depends": ["quilt_loader", {"id": "qsl", "optional": false}, {"id": "maybe", "optional": true}],
			"provides": [{"id": "qalias"}]
		},
		"mixin": "q.mixins.json",
		"minecraft": {"environment": "dedicated_server"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Environment != "server" || !m.Quilt || m.Name != "Q Demo" {
		t.Errorf("fields: %+v", m)
	}
	if m.Entrypoints["client"][0] != "com.q.Client" || m.Entrypoints["main"][0] != "com.q.Main" {
		t.Errorf("entrypoints = %v", m.Entrypoints)
	}
	if !reflect.DeepEqual(m.Depends, []string{"quilt_loader", "qsl"}) {
		t.Errorf("depends = %v", m.Depends)
	}
	if !reflect.DeepEqual(m.Provides, []string{"qalias"}) || m.Mixins[0].Config != "q.mixins.json" {
		t.Errorf("provides/mixins = %v/%v", m.Provides, m.Mixins)
	}
}

const modsToml = `
modLoader="javafml"
loaderVersion="[1,)"
clientSideOnly=true
[[mods]]
modId="demo"
version="${file.jarVersion}"
displayName="Demo"
displayTest="IGNORE_ALL_VERSION"
[[dependencies.demo]]
modId="neoforge"
type="required"
side="CLIENT"
[[dependencies.demo]]
modId="jei"
mandatory=false
side="BOTH"
[[mixins]]
config="demo.mixins.json"
`

func TestParseModsToml(t *testing.T) {
	check := func(t *testing.T, m *ModsToml) {
		t.Helper()
		if !m.ClientSideOnly || len(m.Mods) != 1 || m.Mods[0].ModID != "demo" || m.Mods[0].DisplayTest != "IGNORE_ALL_VERSION" {
			t.Errorf("mods: %+v", m)
		}
		deps := m.Dependencies["demo"]
		if len(deps) != 2 || deps[0] != (ForgeDep{"neoforge", true, "CLIENT"}) || deps[1] != (ForgeDep{"jei", false, "BOTH"}) {
			t.Errorf("deps = %+v", deps)
		}
		if !reflect.DeepEqual(m.Mixins, []string{"demo.mixins.json"}) {
			t.Errorf("mixins = %v", m.Mixins)
		}
	}
	m, err := ParseModsToml([]byte(modsToml), true)
	if err != nil || m.Lenient {
		t.Fatalf("strict parse failed: %v lenient=%v", err, m.Lenient)
	}
	check(t, m)

	// The same data with a TOML syntax error must still be recovered.
	broken, _ := ParseModsToml([]byte(modsToml+"\nlicense=\"unterminated\n"), true)
	if !broken.Lenient {
		t.Fatal("expected lenient fallback")
	}
	check(t, broken)
}

func TestParseMcmodInfo(t *testing.T) {
	for _, src := range []string{
		`[{"modid": "old", "name": "Old Mod", "version": "1.0", "mcversion": "1.12.2",}]`,
		`{"modListVersion": 2, "modList": [{"modid": "old", "name": "Old Mod", "version": "1.0", "mcversion": "1.12.2"}]}`,
	} {
		list, err := ParseMcmodInfo([]byte(src))
		if err != nil || len(list) != 1 || list[0].ModID != "old" || list[0].MC != "1.12.2" {
			t.Errorf("ParseMcmodInfo(%s) = %+v, %v", src, list, err)
		}
	}
}

func TestParseManifest(t *testing.T) {
	m := ParseManifest([]byte("Manifest-Version: 1.0\r\nMixinConfigs: a.mixins.json,b.mix\r\n ins.json\r\nFMLModType: LIBRARY\r\n\r\nName: other/section\r\nX: y\r\n"))
	if m["MixinConfigs"] != "a.mixins.json,b.mixins.json" || m["FMLModType"] != "LIBRARY" || m["X"] != "" {
		t.Errorf("manifest = %v", m)
	}
}

func TestMixinConfigClassPath(t *testing.T) {
	c, err := ParseMixinConfig([]byte(`{"package": "com.demo.mixin", "mixins": ["a.Foo"], "client": ["Bar"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ClassPath("a.Foo"); got != "com/demo/mixin/a/Foo.class" {
		t.Errorf("ClassPath = %s", got)
	}
}
