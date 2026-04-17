package images

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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

// ResolveArticleImage fetches the article page and tries to find a usable
// cover image via Open Graph / Twitter Card / link rel="image_src" meta tags.
// Returns an absolute URL or an empty string if nothing usable was found.
func (f *Fetcher) ResolveArticleImage(ctx context.Context, articleURL string) (string, error) {
	if articleURL == "" {
		return "", nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", articleURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NewsAnalyzer/1.0; +https://github.com/)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en,ru;q=0.9")
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("article %s: %d", articleURL, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	head := string(data)
	if idx := strings.Index(strings.ToLower(head), "</head>"); idx > 0 {
		head = head[:idx]
	}
	props := []string{
		"og:image:secure_url",
		"og:image:url",
		"og:image",
		"twitter:image",
		"twitter:image:src",
	}
	for _, p := range props {
		if v := extractMetaContent(head, p); v != "" {
			return absURL(articleURL, v), nil
		}
	}
	if v := extractLinkImageSrc(head); v != "" {
		return absURL(articleURL, v), nil
	}
	return "", nil
}

var (
	metaRe     = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	linkImgRe  = regexp.MustCompile(`(?is)<link\b[^>]*\brel\s*=\s*["']image_src["'][^>]*>`)
	attrRe     = regexp.MustCompile(`(?is)(\w[\w:-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
)

func extractMetaContent(htmlText, prop string) string {
	prop = strings.ToLower(prop)
	for _, tag := range metaRe.FindAllString(htmlText, -1) {
		attrs := parseAttrs(tag)
		key := attrs["property"]
		if key == "" {
			key = attrs["name"]
		}
		if strings.ToLower(key) != prop {
			continue
		}
		if c := strings.TrimSpace(attrs["content"]); c != "" {
			return html.UnescapeString(c)
		}
	}
	return ""
}

func extractLinkImageSrc(htmlText string) string {
	tag := linkImgRe.FindString(htmlText)
	if tag == "" {
		return ""
	}
	attrs := parseAttrs(tag)
	if h := strings.TrimSpace(attrs["href"]); h != "" {
		return html.UnescapeString(h)
	}
	return ""
}

func parseAttrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
		name := strings.ToLower(m[1])
		val := m[2]
		if val == "" {
			val = m[3]
		}
		if val == "" {
			val = m[4]
		}
		out[name] = val
	}
	return out
}

func absURL(base, ref string) string {
	bu, err := url.Parse(base)
	if err != nil {
		return ref
	}
	ru, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return ref
	}
	return bu.ResolveReference(ru).String()
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
