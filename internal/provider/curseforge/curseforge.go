// Package curseforge is a minimal client for the CurseForge Core API. It
// requires an API key from https://console.curseforge.com/.
package curseforge

import (
	"context"
	"net/http"
	"strconv"

	"github.com/xycld/minecraft-mod-filter/internal/provider"
	"github.com/xycld/minecraft-mod-filter/internal/side"
)

const (
	DefaultBaseURL = "https://api.curseforge.com/v1"
	minecraftGame  = 432
)

type Client struct {
	BaseURL string
	doer    *provider.Doer
}

func New(apiKey, userAgent string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		doer:    provider.NewDoer(http.Header{"X-Api-Key": {apiKey}, "User-Agent": {userAgent}}),
	}
}

type File struct {
	ID           int      `json:"id"`
	ModID        int      `json:"modId"`
	DisplayName  string   `json:"displayName"`
	FileName     string   `json:"fileName"`
	DownloadURL  string   `json:"downloadUrl"`
	Fingerprint  uint32   `json:"fileFingerprint"`
	GameVersions []string `json:"gameVersions"`
	Hashes       []struct {
		Value string `json:"value"`
		Algo  int    `json:"algo"` // 1 = sha1, 2 = md5
	} `json:"hashes"`
}

// SHA1 returns the file's sha1 hash, if CurseForge knows it.
func (f File) SHA1() string {
	for _, h := range f.Hashes {
		if h.Algo == 1 {
			return h.Value
		}
	}
	return ""
}

// Env reads the "Client"/"Server" environment tags authors can attach to a
// file. ok is false when the author set neither.
func (f File) Env() (env side.Env, ok bool) {
	client, server := false, false
	for _, v := range f.GameVersions {
		switch v {
		case "Client":
			client = true
		case "Server":
			server = true
		}
	}
	switch {
	case client && server:
		return side.Env{Client: side.Required, Server: side.Required}, true
	case client:
		// Authors tag "Client" loosely; it does not prove the mod breaks a server.
		return side.Env{Client: side.Required, Server: side.Optional}, true
	case server:
		return side.Env{Client: side.Optional, Server: side.Required}, true
	}
	return side.Env{}, false
}

// FilesByFingerprint matches files by CurseForge fingerprint.
func (c *Client) FilesByFingerprint(ctx context.Context, fingerprints []uint32) (map[uint32]File, error) {
	out := map[uint32]File{}
	for start := 0; start < len(fingerprints); start += 500 {
		chunk := fingerprints[start:min(start+500, len(fingerprints))]
		var res struct {
			Data struct {
				ExactMatches []struct {
					File File `json:"file"`
				} `json:"exactMatches"`
			} `json:"data"`
		}
		body := map[string]any{"fingerprints": chunk}
		if err := c.doer.JSON(ctx, http.MethodPost, c.BaseURL+"/fingerprints/"+strconv.Itoa(minecraftGame), body, &res); err != nil {
			return out, err
		}
		for _, m := range res.Data.ExactMatches {
			out[m.File.Fingerprint] = m.File
		}
	}
	return out, nil
}

// Files fetches files by id (used for CurseForge modpack manifests).
func (c *Client) Files(ctx context.Context, ids []int) (map[int]File, error) {
	out := map[int]File{}
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		var res struct {
			Data []File `json:"data"`
		}
		if err := c.doer.JSON(ctx, http.MethodPost, c.BaseURL+"/mods/files", map[string]any{"fileIds": chunk}, &res); err != nil {
			return out, err
		}
		for _, f := range res.Data {
			out[f.ID] = f
		}
	}
	return out, nil
}

type Mod struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Logo struct {
		URL string `json:"url"`
	} `json:"logo"`
}

// Mods fetches mod (project) records by id.
func (c *Client) Mods(ctx context.Context, ids []int) (map[int]Mod, error) {
	out := map[int]Mod{}
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		var res struct {
			Data []Mod `json:"data"`
		}
		body := map[string]any{"modIds": chunk, "filterPcOnly": true}
		if err := c.doer.JSON(ctx, http.MethodPost, c.BaseURL+"/mods", body, &res); err != nil {
			return out, err
		}
		for _, m := range res.Data {
			out[m.ID] = m
		}
	}
	return out, nil
}
