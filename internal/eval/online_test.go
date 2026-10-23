package eval

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/xycld/minecraft-mod-filter/internal/pack"
	"github.com/xycld/minecraft-mod-filter/internal/provider/modrinth"
	"github.com/xycld/minecraft-mod-filter/internal/scan"
	"github.com/xycld/minecraft-mod-filter/internal/side"
)

// TestSmallishBitsServerPack scans the real client mods of the Smallish Bits
// pack online and compares the mods kept for a server with the pack author's
// official server pack, by Modrinth project. Set MMF_ONLINE=1 to run.
func TestSmallishBitsServerPack(t *testing.T) {
	if os.Getenv("MMF_ONLINE") == "" {
		t.Skip("set MMF_ONLINE=1 to run tests that use the network")
	}
	const dir = "../../testdata/corpus/smallish-bits"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("smallish-bits corpus not present")
	}
	ctx := context.Background()
	mr := modrinth.New("xycld/minecraft-mod-filter/test")

	pk, err := pack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := scan.Run(ctx, pk, scan.Options{Modrinth: mr, Loader: "neoforge"})
	if err != nil {
		t.Fatal(err)
	}

	var index struct {
		Files []struct {
			Hashes map[string]string `json:"hashes"`
		} `json:"files"`
	}
	raw, err := os.ReadFile("../../testdata/corpus/smallish-bits-server.index.json")
	if err != nil {
		t.Skip("official server index not present")
	}
	if err := json.Unmarshal(raw, &index); err != nil {
		t.Fatal(err)
	}
	var hashes []string
	for _, f := range index.Files {
		hashes = append(hashes, f.Hashes["sha1"])
	}
	official, err := mr.VersionsByHash(ctx, "sha1", hashes)
	if err != nil {
		t.Fatal(err)
	}
	onServer := map[string]bool{}
	for _, v := range official {
		onServer[v.ProjectID] = true
	}

	var agree, keptExtra, droppedNeeded int
	var extra, dropped []string
	for _, m := range res.Mods {
		if m.Modrinth == nil || !m.Modrinth.ByHash {
			continue
		}
		keep := m.Verdict.Env.Server != side.Unsupported
		switch official := onServer[m.Modrinth.ProjectID]; {
		case keep == official:
			agree++
		case keep:
			keptExtra++
			extra = append(extra, m.File+" ("+m.Verdict.Category+")")
		default:
			droppedNeeded++
			dropped = append(dropped, m.File+" ("+m.Verdict.Category+")")
		}
	}
	sort.Strings(extra)
	sort.Strings(dropped)
	total := agree + keptExtra + droppedNeeded
	t.Logf("agree with official server pack: %d/%d (%.1f%%)", agree, total, 100*float64(agree)/float64(total))
	t.Logf("kept but not in official pack (%d): %v", keptExtra, extra)
	t.Logf("dropped but in official pack (%d): %v", droppedNeeded, dropped)
	// A stricter server-pack policy: keep only what the server requires.
	strict := 0
	for _, m := range res.Mods {
		if m.Modrinth != nil && m.Modrinth.ByHash && (m.Verdict.Env.Server == side.Required) == onServer[m.Modrinth.ProjectID] {
			strict++
		}
	}
	t.Logf("keep-only-required policy agrees: %d/%d (%.1f%%)", strict, total, 100*float64(strict)/float64(total))
	if droppedNeeded > 3 {
		t.Errorf("dropped %d mods the official server pack needs", droppedNeeded)
	}
}
