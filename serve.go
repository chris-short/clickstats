package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"time"
)

//go:embed web
var webFiles embed.FS

type server struct {
	apiKey string
	name   string
	mux    *http.ServeMux
	cache  *cache
}

func newServer(apiKey, name string) *server {
	s := &server{
		apiKey: apiKey,
		name:   name,
		cache:  newCache(10 * time.Minute),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/issues", s.handleIssues)
	mux.HandleFunc("GET /api/domains", s.handleDomains)
	mux.HandleFunc("GET /api/stats/issue/{n}", s.handleIssueStats)
	mux.HandleFunc("GET /print/issue/{n}", s.handlePrint)
	sub, _ := fs.Sub(webFiles, "web")
	mux.Handle("/", http.FileServerFS(sub))
	s.mux = mux
	return s
}

func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"name": s.name})
}

func runServe(args []string) {
	fset := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fset.Int("port", 8080, "port to listen on")
	host := fset.String("host", "127.0.0.1", "host to listen on")
	defaultName := "DevOps'ish"
	if n := os.Getenv("CLICKSTATS_NAME"); n != "" {
		defaultName = n
	}
	name := fset.String("name", defaultName, "newsletter name shown in dashboard")
	fset.Parse(args)

	apiKey := os.Getenv("BUTTONDOWN_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "BUTTONDOWN_API_KEY not set")
		os.Exit(1)
	}

	s := newServer(apiKey, *name)
	s.warmCache()
	addr := fmt.Sprintf("%s:%d", *host, *port)
	fmt.Printf("clickstats listening on http://%s (warming cache in background)\n", addr)
	if err := http.ListenAndServe(addr, s.mux); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
