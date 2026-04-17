package images

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Fetcher struct {
	Dir        string
	PublicBase string
	HTTP       *http.Client
}

func New(dir, publicBase string) *Fetcher {
	return &Fetcher{Dir: dir, PublicBase: publicBase, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Fetch downloads the image URL into <Dir>/<runID>/<hash>.<ext>.
// Returns the local filesystem path and the public URL path (e.g. /images/<runID>/<hash>.<ext>).
func (f *Fetcher) Fetch(ctx context.Context, runID, url string) (string, string, error) {
	if url == "" {
		return "", "", nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("image %s: %d", url, resp.StatusCode)
	}
	ext := extFromContentType(resp.Header.Get("Content-Type"))
	if ext == "" {
		ext = extFromURL(url)
	}
	if ext == "" {
		ext = ".jpg"
	}
	sum := sha1.Sum([]byte(url))
	name := hex.EncodeToString(sum[:]) + ext
	dir := filepath.Join(f.Dir, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	full := filepath.Join(dir, name)
	out, err := os.Create(full)
	if err != nil {
		return "", "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", "", err
	}
	public := strings.TrimRight(f.PublicBase, "/") + "/images/" + runID + "/" + name
	return full, public, nil
}

func extFromContentType(ct string) string {
	ct = strings.ToLower(strings.Split(ct, ";")[0])
	switch strings.TrimSpace(ct) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	}
	return ""
}

func extFromURL(u string) string {
	u = strings.ToLower(u)
	if i := strings.LastIndex(u, "."); i > 0 {
		ext := u[i:]
		if j := strings.IndexAny(ext, "?#"); j > 0 {
			ext = ext[:j]
		}
		switch ext {
		case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".svg":
			if ext == ".jpeg" {
				return ".jpg"
			}
			return ext
		}
	}
	return ""
}
