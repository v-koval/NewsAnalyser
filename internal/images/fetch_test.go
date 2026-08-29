package images

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchRejectsBlockedScheme(t *testing.T) {
	f := &Fetcher{Dir: t.TempDir(), HTTP: http.DefaultClient}
	_, _, err := f.Fetch(context.Background(), "run1", "file:///etc/passwd")
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err = %v, want scheme error", err)
	}
}

func TestFetchRejectsNonImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer srv.Close()
	f := &Fetcher{Dir: t.TempDir(), HTTP: srv.Client()}
	_, _, err := f.Fetch(context.Background(), "run1", srv.URL+"/a.jpg")
	if err == nil || !strings.Contains(err.Error(), "content type") {
		t.Fatalf("err = %v, want content type error", err)
	}
}

func TestFetchRejectsOversized(t *testing.T) {
	big := bytes.Repeat([]byte{0}, maxImageBytes+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(big)
	}))
	defer srv.Close()
	dir := t.TempDir()
	f := &Fetcher{Dir: dir, HTTP: srv.Client()}
	_, _, err := f.Fetch(context.Background(), "run1", srv.URL+"/big.png")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want size error", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "run1"))
	if len(entries) != 0 {
		t.Fatalf("oversized download left %d files on disk", len(entries))
	}
}
