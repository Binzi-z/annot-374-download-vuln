// Package main implements a small file download service.
//
// The service has two entry points:
//
//	GET /download?url=<remote url>   fetch a remote asset and stream it back
//	GET /file?name=<relative path>   stream a file from the local asset directory
//
// Both are used by the internal portal. The audit team reported that the first
// one can be pointed at internal addresses and that the second one escapes the
// asset directory, but nobody has produced a stable reproduction yet.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8094", "listen address")
	assetDir := flag.String("assets", "assets", "directory served by /file")
	internalAddr := flag.String("internal", "", "address of the internal metadata service (test hook)")
	flag.Parse()

	if err := os.MkdirAll(*assetDir, 0o755); err != nil {
		log.Fatalf("asset dir: %v", err)
	}

	app := NewApp(AppOptions{
		AssetDir:     *assetDir,
		InternalAddr: *internalAddr,
	})
	srv := &http.Server{Addr: *addr, Handler: app.Routes()}
	log.Printf("download service listening on %s (assets=%s)", *addr, *assetDir)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}
