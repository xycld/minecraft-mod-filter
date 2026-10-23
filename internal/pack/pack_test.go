package pack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xycld/minecraft-mod-filter/internal/side"
	"github.com/xycld/minecraft-mod-filter/internal/testjar"
)

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "mods", "a.jar"), testjar.New().File("x", "y").Bytes())
	write(t, filepath.Join(dir, "mods", "notes.txt"), []byte("ignored"))
	// Pointing at the instance folder finds mods/ inside it.
	pk, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pk.Format != "directory" || len(pk.Items) != 1 || pk.Items[0].Path != "a.jar" || pk.Items[0].Load == nil {
		t.Errorf("pack = %+v", pk)
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Error("expected an error for a folder without jars")
	}
}

func TestLoadMrpack(t *testing.T) {
	index := `{
		"formatVersion": 1, "game": "minecraft", "name": "Demo Pack", "versionId": "1.0",
		"dependencies": {"minecraft": "1.21.1", "neoforge": "21.1.1"},
		"files": [
			{"path": "mods/sodium.jar", "hashes": {"sha1": "aa", "sha512": "bb"}, "env": {"client": "required", "server": "unsupported"},
			 "downloads": ["https://cdn.modrinth.com/x/sodium.jar"], "fileSize": 10},
			{"path": "resourcepacks/pack.zip", "hashes": {"sha1": "cc"}, "downloads": []},
			{"path": "mods/nested/odd.jar", "hashes": {"sha1": "dd"}, "downloads": []}
		]
	}`
	mr := testjar.New().
		File("modrinth.index.json", index).
		Raw("overrides/mods/local.jar", testjar.New().File("a", "b").Bytes()).
		Raw("server-overrides/mods/srv.jar", testjar.New().File("a", "c").Bytes())
	path := filepath.Join(t.TempDir(), "demo.mrpack")
	write(t, path, mr.Bytes())

	pk, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if pk.Name != "Demo Pack" || pk.Loader != "neoforge" || pk.GameVersion != "1.21.1" {
		t.Errorf("pack meta = %+v", pk)
	}
	if len(pk.Items) != 3 {
		t.Fatalf("items = %+v", pk.Items)
	}
	byPath := map[string]Item{}
	for _, it := range pk.Items {
		byPath[it.Path] = it
	}
	s := byPath["sodium.jar"]
	if s.SHA1 != "aa" || s.Load != nil || s.Declared == nil || *s.Declared != (side.Env{Client: side.Required, Server: side.Unsupported}) {
		t.Errorf("sodium item = %+v", s)
	}
	defer pk.Close()
	if data, err := byPath["local.jar"].Load(); err != nil || len(data) == 0 {
		t.Errorf("override jar not readable: %v", err)
	}
	if d := byPath["srv.jar"].Declared; d == nil || d.Client != side.Unsupported {
		t.Errorf("server-override declared = %+v", d)
	}
}

func TestLoadCurseForge(t *testing.T) {
	manifest := `{
		"manifestType": "minecraftModpack", "name": "CF Pack", "version": "2",
		"minecraft": {"version": "1.20.1", "modLoaders": [{"id": "forge-47.2.0", "primary": true}]},
		"files": [{"projectID": 238222, "fileID": 4712345, "required": true}],
		"overrides": "overrides"
	}`
	path := filepath.Join(t.TempDir(), "cf.zip")
	write(t, path, testjar.New().File("manifest.json", manifest).Raw("overrides/mods/x.jar", testjar.New().File("a", "b").Bytes()).Bytes())
	pk, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	defer pk.Close() // Windows cannot delete the temp dir while the archive is open
	if pk.Loader != "forge" || pk.GameVersion != "1.20.1" || len(pk.Items) != 2 {
		t.Errorf("pack = %+v", pk)
	}
	if pk.Items[0].CurseForgeProject != 238222 || pk.Items[0].CurseForgeFile != 4712345 {
		t.Errorf("manifest item = %+v", pk.Items[0])
	}
}
