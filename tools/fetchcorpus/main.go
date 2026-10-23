// Command fetchcorpus builds a labeled corpus of real mod jars from Modrinth.
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	apiBase   = "https://api.modrinth.com/v2"
	userAgent = "xycld/minecraft-mod-filter-corpus/0.1 (github.com/xycld/minecraft-mod-filter)"
	maxSize   = 25 << 20
	workers   = 4
)

var httpClient = &http.Client{Timeout: 120 * time.Second}

// rate limiting state shared by all requests.
var (
	rlMu    sync.Mutex
	rlUntil time.Time // no requests before this instant
)

// get performs a rate-limited GET, retrying on 429 and transient errors.
func get(u string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		rlMu.Lock()
		wait := time.Until(rlUntil)
		rlMu.Unlock()
		if wait > 0 {
			time.Sleep(wait)
		}
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("User-Agent", userAgent)
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}
		reset := time.Duration(0)
		if s := resp.Header.Get("X-Ratelimit-Reset"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				reset = time.Duration(n) * time.Second
			}
		}
		pause := time.Duration(0)
		if resp.StatusCode == 429 {
			pause = reset
			if pause == 0 {
				pause = 10 * time.Second
			}
			pause += time.Second
			log.Printf("429 from %s, sleeping %v", u, pause)
		} else if n, err := strconv.Atoi(resp.Header.Get("X-Ratelimit-Remaining")); err == nil && n < 10 {
			pause = reset + 500*time.Millisecond
		}
		if pause > 0 {
			rlMu.Lock()
			if t := time.Now().Add(pause); t.After(rlUntil) {
				rlUntil = t
			}
			rlMu.Unlock()
		}
		if resp.StatusCode == 429 {
			resp.Body.Close()
			continue
		}
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("%s: %s", u, resp.Status)
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("%s: %s", u, resp.Status)
		}
		return resp, nil
	}
	return nil, fmt.Errorf("giving up on %s: %v", u, lastErr)
}

func getJSON(u string, v any) error {
	resp, err := get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

type hit struct {
	ProjectID string `json:"project_id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
}

// search paginates a facet query until max hits or results run out.
func search(facets [][]string, max int) ([]hit, error) {
	fj, _ := json.Marshal(facets)
	var out []hit
	for offset := 0; offset < max; offset += 100 {
		q := url.Values{"facets": {string(fj)}, "index": {"downloads"}, "limit": {"100"}, "offset": {strconv.Itoa(offset)}}
		var r struct {
			Hits      []hit `json:"hits"`
			TotalHits int   `json:"total_hits"`
		}
		if err := getJSON(apiBase+"/search?"+q.Encode(), &r); err != nil {
			return out, err
		}
		out = append(out, r.Hits...)
		if len(r.Hits) < 100 || offset+100 >= r.TotalHits {
			break
		}
	}
	return out, nil
}

type file struct {
	Hashes   map[string]string `json:"hashes"`
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Size     int64             `json:"size"`
}

type version struct {
	ID          string `json:"id"`
	DatePub     string `json:"date_published"`
	Environment string `json:"environment"`
	Files       []file `json:"files"`
}

// Label is one entry of labels.json.
type Label struct {
	File        string `json:"file"`
	SHA1        string `json:"sha1"`
	ProjectID   string `json:"project_id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	VersionID   string `json:"version_id"`
	Environment string `json:"environment"`
	url         string
}

// resolve finds the newest matching version of a project and its jar.
func resolve(h hit, loader, gv string) (*Label, error) {
	q := url.Values{"loaders": {`["` + loader + `"]`}, "game_versions": {`["` + gv + `"]`}}
	var vs []version
	if err := getJSON(apiBase+"/project/"+h.ProjectID+"/version?"+q.Encode(), &vs); err != nil {
		return nil, err
	}
	if len(vs) == 0 {
		return nil, nil
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i].DatePub > vs[j].DatePub })
	v := vs[0]
	if v.Environment == "" || v.Environment == "unknown" || len(v.Files) == 0 {
		return nil, nil
	}
	f := v.Files[0]
	for _, c := range v.Files {
		if c.Primary {
			f = c
			break
		}
	}
	name := filepath.Base(f.Filename)
	if !strings.HasSuffix(strings.ToLower(name), ".jar") || f.Size > maxSize || f.Hashes["sha1"] == "" {
		return nil, nil
	}
	return &Label{File: name, SHA1: f.Hashes["sha1"], ProjectID: h.ProjectID, Slug: h.Slug, Title: h.Title,
		VersionID: v.ID, Environment: v.Environment, url: f.URL}, nil
}

func bucket(env string) int {
	switch env {
	case "client_only", "client_only_server_optional":
		return 0
	case "server_only", "server_only_client_optional", "dedicated_server_only":
		return 1
	}
	return 2
}

// pick selects up to n labels, balanced across the three buckets.
func pick(all []*Label, n int) []*Label {
	var b [3][]*Label
	for _, l := range all {
		b[bucket(l.Environment)] = append(b[bucket(l.Environment)], l)
	}
	var out []*Label
	idx := [3]int{}
	for len(out) < n {
		progressed := false
		for i := 0; i < 3 && len(out) < n; i++ {
			if idx[i] < len(b[i]) {
				out = append(out, b[i][idx[i]])
				idx[i]++
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}
	return out
}

func fileSHA1(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func download(l *Label, dir string) error {
	dst := filepath.Join(dir, l.File)
	if fileSHA1(dst) == l.SHA1 {
		return nil
	}
	resp, err := get(l.url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha1.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	f.Close()
	if err == nil && hex.EncodeToString(h.Sum(nil)) != l.SHA1 {
		err = fmt.Errorf("sha1 mismatch for %s", l.File)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// parallel runs fn over 0..n-1 with bounded concurrency.
func parallel(n int, fn func(i int)) {
	var wg sync.WaitGroup
	ch := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		ch <- i
	}
	close(ch)
	wg.Wait()
}

func buildSet(out, set string, perSet int) error {
	loader, gv, ok := strings.Cut(set, "-")
	if !ok {
		return fmt.Errorf("bad set %q, want <loader>-<gameversion>", set)
	}
	base := [][]string{{"project_type:mod"}, {"categories:" + loader}, {"versions:" + gv}}
	queries := []struct {
		extra [][]string
		max   int
	}{
		{nil, 300},
		{[][]string{{"client_side:required"}, {"server_side:unsupported"}}, 200},
		{[][]string{{"server_side:required"}, {"client_side:unsupported"}}, 200},
	}
	seen := map[string]bool{}
	var pool []hit
	for _, q := range queries {
		hits, err := search(append(append([][]string{}, base...), q.extra...), q.max)
		if err != nil {
			return err
		}
		for _, h := range hits {
			if !seen[h.ProjectID] {
				seen[h.ProjectID] = true
				pool = append(pool, h)
			}
		}
	}
	log.Printf("[%s] %d candidates", set, len(pool))

	resolved := make([]*Label, len(pool))
	var failed int
	var mu sync.Mutex
	parallel(len(pool), func(i int) {
		l, err := resolve(pool[i], loader, gv)
		if err != nil {
			log.Printf("[%s] resolve %s: %v", set, pool[i].Slug, err)
			mu.Lock()
			failed++
			mu.Unlock()
		}
		resolved[i] = l
	})
	var all []*Label
	for _, l := range resolved {
		if l != nil {
			all = append(all, l)
		}
	}
	log.Printf("[%s] %d usable (%d resolve errors)", set, len(all), failed)

	// Filenames must be unique within a set.
	names := map[string]bool{}
	var uniq []*Label
	for _, l := range all {
		if !names[l.File] {
			names[l.File] = true
			uniq = append(uniq, l)
		}
	}
	chosen := pick(uniq, perSet)

	dir := filepath.Join(out, set)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var got []Label
	done := make([]bool, len(chosen))
	parallel(len(chosen), func(i int) {
		if err := download(chosen[i], dir); err != nil {
			log.Printf("[%s] download %s: %v", set, chosen[i].File, err)
			return
		}
		done[i] = true
	})
	for i, l := range chosen {
		if done[i] {
			got = append(got, *l)
		}
	}
	sort.Slice(got, func(i, j int) bool { return got[i].File < got[j].File })
	data, _ := json.MarshalIndent(got, "", "  ")
	log.Printf("[%s] %d/%d jars written", set, len(got), len(chosen))
	return os.WriteFile(filepath.Join(dir, "labels.json"), append(data, '\n'), 0o644)
}

func main() {
	out := flag.String("out", "testdata/corpus", "output directory")
	perSet := flag.Int("per-set", 60, "max jars per set")
	sets := flag.String("sets", "fabric-1.20.1", "comma-separated <loader>-<gameversion> sets")
	flag.Parse()
	log.SetFlags(log.Ltime)
	for _, set := range strings.Split(*sets, ",") {
		if err := buildSet(*out, strings.TrimSpace(set), *perSet); err != nil {
			log.Fatalf("fatal: %s: %v", set, err)
		}
	}
}
