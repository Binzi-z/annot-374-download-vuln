package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// AppOptions configures the service.
type AppOptions struct {
	AssetDir     string
	InternalAddr string // used by tests to stand up an "internal" service
	HTTPClient   *http.Client
}

// App holds the handlers.
type App struct {
	opts   AppOptions
	client *http.Client
}

// NewApp builds the application.
func NewApp(opts AppOptions) *App {
	c := opts.HTTPClient
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	return &App{opts: opts, client: c}
}

// Routes wires the HTTP handlers.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/download", a.handleDownload)
	mux.HandleFunc("/file", a.handleFile)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	})
	return mux
}

// handleDownload fetches a remote asset and streams it back.
func (a *App) handleDownload(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("url")
	if target == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	resp, err := a.client.Get(target)
	if err != nil {
		http.Error(w, "fetch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, io.LimitReader(resp.Body, 64<<20))
}

// handleFile streams a file from the asset directory.
func (a *App) handleFile(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	full := filepath.Join(a.opts.AssetDir, name)
	f, err := os.Open(full)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.Error(w, "not a file", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

// isLoopback is a helper the tests use; the service itself does not call it.
func isLoopback(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1" || ip == "localhost"
}
