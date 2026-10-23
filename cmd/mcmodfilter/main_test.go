package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xycld/minecraft-mod-filter/internal/testjar"
)

func modsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	jars := map[string][]byte{
		"sodium-like.jar":   testjar.New().File("fabric.mod.json", `{"id":"sodiumlike","name":"Sodium Like","environment":"client"}`).Bytes(),
		"worldgen-like.jar": testjar.New().File("fabric.mod.json", `{"id":"worldgen","name":"Worldgen Like"}`).File("data/worldgen/worldgen/structure/a.json", "{}").Bytes(),
	}
	for name, data := range jars {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRunTable(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"-offline", "-lang", "en", modsDir(t)}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	s := out.String()
	for _, want := range []string{"Sodium Like", "Worldgen Like", "Remove from a dedicated server (1):", "sodium-like.jar", "2 mods scanned (offline)"} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
}

func TestRunJSONChinese(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-offline", "-format", "json", modsDir(t)}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var res struct {
		Mods []struct {
			File    string
			Verdict struct{ Category string }
		}
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range res.Mods {
		got[m.File] = m.Verdict.Category
	}
	if got["sodium-like.jar"] != "client-only" || got["worldgen-like.jar"] != "server (client optional)" {
		t.Errorf("categories = %v", got)
	}

	out.Reset()
	run([]string{"-offline", "-lang", "zh-CN", modsDir(t)}, &out, &errOut)
	if !strings.Contains(out.String(), "应从专用服务器移除") {
		t.Errorf("Chinese output missing:\n%s", out.String())
	}
}

func TestRunErrors(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-format", "xml", "."}, &out, &errOut); code != 2 {
		t.Errorf("bad format exit = %d", code)
	}
	if code := run([]string{"-offline", filepath.Join(t.TempDir(), "missing")}, &out, &errOut); code != 1 {
		t.Errorf("missing path exit = %d", code)
	}
	if code := run([]string{"-version"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "mcmodfilter") {
		t.Errorf("-version: %d %q", code, out.String())
	}
}
