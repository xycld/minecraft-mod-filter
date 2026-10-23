package scan

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xycld/minecraft-mod-filter/internal/pack"
)

// Hosts files referenced by a pack may be downloaded from: the .mrpack
// specification's list plus CurseForge's CDNs. Redirects must stay on them.
var allowedHosts = []string{
	"cdn.modrinth.com", "github.com", "raw.githubusercontent.com", "gitlab.com",
	"edge.forgecdn.net", "mediafilez.forgecdn.net",
}

// Redirect targets of the hosts above (GitHub release assets, CDN mirrors).
var allowedHostSuffixes = []string{".githubusercontent.com", ".forgecdn.net"}

func hostAllowed(u *url.URL) bool {
	if u.Scheme != "https" {
		return false
	}
	h := u.Hostname()
	for _, a := range allowedHosts {
		if h == a {
			return true
		}
	}
	for _, s := range allowedHostSuffixes {
		if strings.HasSuffix(h, s) {
			return true
		}
	}
	return false
}

var downloadClient = &http.Client{
	Timeout: 5 * time.Minute,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if !hostAllowed(req.URL) {
			return fmt.Errorf("redirect to disallowed host %s", req.URL.Host)
		}
		return nil
	},
}

const maxDownload = 512 << 20

// loadItem returns a pack item's bytes: read from disk or the pack archive,
// or, when download is set, fetched from the pack's URLs and checked against
// the hash the pack index declares. It returns nil, nil for referenced files
// that are not downloaded.
func loadItem(ctx context.Context, it pack.Item, download bool) ([]byte, error) {
	if it.Load != nil {
		return it.Load()
	}
	if !download || len(it.URLs) == 0 {
		return nil, nil
	}
	if it.SHA1 == "" && it.SHA512 == "" {
		return nil, errors.New("not downloaded: the pack declares no hash for it")
	}
	var lastErr error
	for _, raw := range it.URLs {
		u, err := url.Parse(raw)
		if err != nil || !hostAllowed(u) {
			lastErr = fmt.Errorf("download host not allowed: %s", raw)
			continue
		}
		data, err := fetch(ctx, raw)
		if err == nil {
			err = verify(data, it.SHA1, it.SHA512)
		}
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("download failed: %w", lastErr)
}

func fetch(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err == nil && len(data) > maxDownload {
		err = errors.New("file too large")
	}
	return data, err
}

func verify(data []byte, sha1sum, sha512sum string) error {
	if sha512sum != "" {
		h := sha512.Sum512(data)
		if !strings.EqualFold(hex.EncodeToString(h[:]), sha512sum) {
			return errors.New("sha512 mismatch")
		}
		return nil
	}
	if !strings.EqualFold(sha1Hex(data), sha1sum) {
		return errors.New("sha1 mismatch")
	}
	return nil
}
