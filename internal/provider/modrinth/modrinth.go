// Package modrinth is a minimal client for the Modrinth v2 API.
package modrinth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/xycld/minecraft-mod-filter/internal/provider"
)

const DefaultBaseURL = "https://api.modrinth.com/v2"

type Client struct {
	BaseURL string
	doer    *provider.Doer
}

func New(userAgent string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		doer:    provider.NewDoer(http.Header{"User-Agent": {userAgent}}),
	}
}

type File struct {
	Hashes   map[string]string `json:"hashes"`
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Size     int64             `json:"size"`
}

type Version struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"version_number"`
	Environment   string   `json:"environment"`
	Loaders       []string `json:"loaders"`
	GameVersions  []string `json:"game_versions"`
	Files         []File   `json:"files"`
}

type Project struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	ProjectType string `json:"project_type"`
	ClientSide  string `json:"client_side"`
	ServerSide  string `json:"server_side"`
	IconURL     string `json:"icon_url"`
}

// VersionsByHash looks up versions by file hash ("sha1" or "sha512").
// Hashes without a match are absent from the result.
func (c *Client) VersionsByHash(ctx context.Context, algorithm string, hashes []string) (map[string]Version, error) {
	out := map[string]Version{}
	for start := 0; start < len(hashes); start += 500 {
		chunk := hashes[start:min(start+500, len(hashes))]
		var res map[string]Version
		body := map[string]any{"hashes": chunk, "algorithm": algorithm}
		if err := c.doer.JSON(ctx, http.MethodPost, c.BaseURL+"/version_files", body, &res); err != nil {
			return out, err
		}
		for h, v := range res {
			out[h] = v
		}
	}
	return out, nil
}

// Projects fetches projects by id.
func (c *Client) Projects(ctx context.Context, ids []string) (map[string]Project, error) {
	out := map[string]Project{}
	for start := 0; start < len(ids); start += 100 {
		chunk := ids[start:min(start+100, len(ids))]
		q, _ := json.Marshal(chunk)
		var res []Project
		if err := c.doer.JSON(ctx, http.MethodGet, c.BaseURL+"/projects?ids="+url.QueryEscape(string(q)), nil, &res); err != nil {
			return out, err
		}
		for _, p := range res {
			out[p.ID] = p
		}
	}
	return out, nil
}

// Project fetches a project by id or slug. It returns nil, nil if none exists.
func (c *Client) Project(ctx context.Context, idOrSlug string) (*Project, error) {
	var p Project
	err := c.doer.JSON(ctx, http.MethodGet, c.BaseURL+"/project/"+url.PathEscape(idOrSlug), nil, &p)
	if errors.Is(err, provider.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ProjectVersions lists a project's versions, newest first, optionally
// filtered by loader and game version.
func (c *Client) ProjectVersions(ctx context.Context, project, loader, gameVersion string) ([]Version, error) {
	q := url.Values{}
	if loader != "" {
		b, _ := json.Marshal([]string{loader})
		q.Set("loaders", string(b))
	}
	if gameVersion != "" {
		b, _ := json.Marshal([]string{gameVersion})
		q.Set("game_versions", string(b))
	}
	var vs []Version
	err := c.doer.JSON(ctx, http.MethodGet, c.BaseURL+"/project/"+url.PathEscape(project)+"/version?"+q.Encode(), nil, &vs)
	if errors.Is(err, provider.ErrNotFound) {
		return nil, nil
	}
	return vs, err
}
