package classfile

import (
	"slices"
	"strings"
	"testing"

	"github.com/xycld/minecraft-mod-filter/internal/testjar"
)

func TestParse(t *testing.T) {
	data := testjar.Class{
		Name: "com/example/ExampleMod",
		Refs: []string{"net/minecraft/client/Minecraft", "[Lnet/minecraft/world/item/Item;"},
		Annotations: []testjar.Annotation{{
			Type: "net/neoforged/fml/common/Mod",
			Values: map[string]any{
				"value": "example",
				"dist":  []any{testjar.Enum{Type: "net/neoforged/api/distmarker/Dist", Name: "CLIENT"}},
				"flag":  true,
			},
		}},
	}.Bytes()

	c, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "com/example/ExampleMod" || c.Super != "java/lang/Object" {
		t.Errorf("name/super = %q/%q", c.Name, c.Super)
	}
	for _, want := range []string{"net/minecraft/client/Minecraft", "net/minecraft/world/item/Item"} {
		if !slices.Contains(c.Refs, want) {
			t.Errorf("refs %v missing %s", c.Refs, want)
		}
	}
	a, ok := c.Find("net/neoforged/fml/common/Mod")
	if !ok {
		t.Fatal("@Mod annotation not found")
	}
	if a.Values["value"] != "example" || a.Values["flag"] != true {
		t.Errorf("values = %v", a.Values)
	}
	dist, _ := a.Values["dist"].([]any)
	if len(dist) != 1 || dist[0] != (Enum{Type: "net/neoforged/api/distmarker/Dist", Name: "CLIENT"}) {
		t.Errorf("dist = %v", a.Values["dist"])
	}
	if !c.HasRefPrefix("net/minecraft/client/") {
		t.Error("HasRefPrefix(client) = false")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("not a class")); err == nil {
		t.Error("expected error for non-class data")
	}
	full := testjar.Class{Name: "a/B", Refs: []string{"c/D"}}.Bytes()
	for n := 0; n < len(full); n++ {
		if _, err := Parse(full[:n]); err == nil {
			t.Fatalf("truncated class (%d of %d bytes) parsed without error", n, len(full))
		}
	}
}

// FuzzParse checks that arbitrary bytes never panic: class files come from
// untrusted jars.
func FuzzParse(f *testing.F) {
	f.Add(testjar.Class{Name: "a/B", Refs: []string{"net/minecraft/client/C"}}.Bytes())
	f.Add([]byte{0xCA, 0xFE, 0xBA, 0xBE, 0, 0, 0, 52, 0xFF, 0xFF})
	f.Fuzz(func(t *testing.T, data []byte) {
		Parse(data)
	})
}

func TestHostileInputIsBounded(t *testing.T) {
	// 1000 nested arrays: must fail fast, not recurse or allocate per level.
	nested := []any{"x"}
	for i := 0; i < 1000; i++ {
		nested = []any{nested}
	}
	// The class still parses; only the malformed annotation block is dropped.
	c, err := Parse(testjar.Class{Name: "a/B", Annotations: []testjar.Annotation{{Type: "x/A", Values: map[string]any{"v": nested}}}}.Bytes())
	if err != nil || len(c.Annotations) != 0 {
		t.Errorf("deeply nested annotation: err=%v annotations=%d", err, len(c.Annotations))
	}

	// A huge Utf8 constant without a terminator must not be scanned quadratically.
	var refs []string
	descriptorRefs(strings.Repeat("L", 1<<20)+";", func(s string) { refs = append(refs, s) })
	if len(refs) != 0 {
		t.Errorf("got %d refs from a name without a package", len(refs))
	}
	descriptorRefs("(ILjava/util/List<Lfoo/Bar;>;[Lbaz/Q;)V", func(s string) { refs = append(refs, s) })
	if !slices.Equal(refs, []string{"java/util/List", "foo/Bar", "baz/Q"}) {
		t.Errorf("refs = %v", refs)
	}
}
