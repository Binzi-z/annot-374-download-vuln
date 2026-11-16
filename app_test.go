package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestApp builds an app with a temporary asset dir containing two files.
func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte("quarterly report"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "nested.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewApp(AppOptions{AssetDir: dir}), dir
}

func doReq(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(body)
}

func TestHealthz(t *testing.T) {
	app, _ := newTestApp(t)
	code, body := doReq(t, app.Routes(), "/healthz")
	if code != 200 || body != "ok" {
		t.Fatalf("healthz: %d %q", code, body)
	}
}

func TestFileServesAsset(t *testing.T) {
	app, _ := newTestApp(t)
	code, body := doReq(t, app.Routes(), "/file?name=report.txt")
	if code != 200 || body != "quarterly report" {
		t.Fatalf("file: %d %q", code, body)
	}
}

func TestFileServesNestedAsset(t *testing.T) {
	app, _ := newTestApp(t)
	code, body := doReq(t, app.Routes(), "/file?name=sub/nested.txt")
	if code != 200 || body != "nested" {
		t.Fatalf("nested: %d %q", code, body)
	}
}

func TestFileMissingNameIs400(t *testing.T) {
	app, _ := newTestApp(t)
	if code, _ := doReq(t, app.Routes(), "/file"); code != 400 {
		t.Fatalf("want 400, got %d", code)
	}
}

func TestFileUnknownNameIs404(t *testing.T) {
	app, _ := newTestApp(t)
	if code, _ := doReq(t, app.Routes(), "/file?name=absent.txt"); code != 404 {
		t.Fatalf("want 404, got %d", code)
	}
}

func TestFileDirectoryIsRejected(t *testing.T) {
	app, _ := newTestApp(t)
	if code, _ := doReq(t, app.Routes(), "/file?name=sub"); code == 200 {
		t.Fatalf("directory must not be served, got %d", code)
	}
}

func TestFileTraversalOutsideAssetDirIsBlocked(t *testing.T) {
	// A file that exists one level above the asset dir must not be reachable.
	app, dir := newTestApp(t)
	outside := filepath.Join(filepath.Dir(dir), "secret.txt")
	if err := os.WriteFile(outside, []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)

	code, body := doReq(t, app.Routes(), "/file?name=../secret.txt")
	if code == 200 || strings.Contains(body, "SECRET") {
		t.Fatalf("path traversal leaked: %d %q", code, body)
	}
}

func TestFileEncodedTraversalIsBlocked(t *testing.T) {
	app, dir := newTestApp(t)
	outside := filepath.Join(filepath.Dir(dir), "secret2.txt")
	if err := os.WriteFile(outside, []byte("SECRET2"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)

	code, body := doReq(t, app.Routes(), "/file?name=..%2Fsecret2.txt")
	if code == 200 || strings.Contains(body, "SECRET2") {
		t.Fatalf("encoded traversal leaked: %d %q", code, body)
	}
}

func TestDownloadMissingURLIs400(t *testing.T) {
	app, _ := newTestApp(t)
	if code, _ := doReq(t, app.Routes(), "/download"); code != 400 {
		t.Fatalf("want 400, got %d", code)
	}
}

func TestDownloadFetchesRemoteAsset(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "upstream body")
	}))
	defer upstream.Close()

	app, _ := newTestApp(t)
	code, body := doReq(t, app.Routes(), "/download?url="+upstream.URL)
	if code != 200 || body != "upstream body" {
		t.Fatalf("download: %d %q", code, body)
	}
}

func TestDownloadPropagatesUpstreamStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	defer upstream.Close()

	app, _ := newTestApp(t)
	if code, _ := doReq(t, app.Routes(), "/download?url="+upstream.URL); code != http.StatusGone {
		t.Fatalf("want 410, got %d", code)
	}
}

func TestDownloadUpstreamUnreachableIs502(t *testing.T) {
	app, _ := newTestApp(t)
	if code, _ := doReq(t, app.Routes(), "/download?url=http://127.0.0.1:1/nope"); code != 502 {
		t.Fatalf("want 502, got %d", code)
	}
}

func TestDownloadRejectsInternalAddress(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "INTERNAL-METADATA")
	}))
	defer internal.Close()

	app, _ := newTestApp(t)
	code, body := doReq(t, app.Routes(), "/download?url="+internal.URL)
	if code == 200 && strings.Contains(body, "INTERNAL-METADATA") {
		t.Fatalf("SSRF read the internal service: %d %q", code, body)
	}
}

func TestDownloadRejectsLoopbackByIP(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "INTERNAL")
	}))
	defer internal.Close()

	app, _ := newTestApp(t)
	host := strings.TrimPrefix(internal.URL, "http://")
	code, body := doReq(t, app.Routes(), "/download?url=http://localhost:"+strings.Split(host, ":")[1]+"/")
	if code == 200 && strings.Contains(body, "INTERNAL") {
		t.Fatalf("loopback by name reached internal service: %d %q", code, body)
	}
}

func TestDownloadRejectsFileScheme(t *testing.T) {
	app, dir := newTestApp(t)
	secret := filepath.Join(dir, "report.txt")
	code, body := doReq(t, app.Routes(), "/download?url=file://"+secret)
	if code == 200 && strings.Contains(body, "quarterly") {
		t.Fatalf("file scheme read local file: %d %q", code, body)
	}
}

func TestDownloadLargeBodyIsCapped(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 1<<20))
	}))
	defer upstream.Close()

	app, _ := newTestApp(t)
	code, body := doReq(t, app.Routes(), "/download?url="+upstream.URL)
	if code != 200 || len(body) > 64<<20 {
		t.Fatalf("large body: %d %d bytes", code, len(body))
	}
}

func TestFileWithSpacesInName(t *testing.T) {
	app, dir := newTestApp(t)
	if err := os.WriteFile(filepath.Join(dir, "a b.txt"), []byte("spaced"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, body := doReq(t, app.Routes(), "/file?name=a%20b.txt")
	if code != 200 || body != "spaced" {
		t.Fatalf("space name: %d %q", code, body)
	}
}

func TestFileRangeRequest(t *testing.T) {
	app, _ := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/file?name=report.txt", nil)
	req.Header.Set("Range", "bytes=0-6")
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("range: want 206, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "quarter" {
		t.Fatalf("range body: %q", got)
	}
}

func TestFileHeadRequest(t *testing.T) {
	app, _ := newTestApp(t)
	req := httptest.NewRequest(http.MethodHead, "/file?name=report.txt", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("head: %d body=%d", rec.Code, rec.Body.Len())
	}
}

func TestIsLoopbackHelper(t *testing.T) {
	if !isLoopback("127.0.0.1") || !isLoopback("localhost") || isLoopback("10.0.0.1") {
		t.Fatal("isLoopback helper is wrong")
	}
}
