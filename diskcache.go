package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	allClicksTTL   = time.Hour
	issueClicksTTL = 7 * 24 * time.Hour
)

type issueEntry struct {
	Clicks map[string]int `json:"c"`
	Saved  time.Time      `json:"s"`
}

type diskData struct {
	AllClicks   map[string]int        `json:"ac,omitempty"`
	AllSaved    time.Time             `json:"as,omitempty"`
	IssueClicks map[string]issueEntry `json:"ic,omitempty"`
}

type diskCache struct {
	mu   sync.Mutex
	path string
	d    diskData
}

func newDiskCache(dir string) *diskCache {
	dc := &diskCache{
		path: filepath.Join(dir, "cache.json"),
		d: diskData{
			AllClicks:   map[string]int{},
			IssueClicks: map[string]issueEntry{},
		},
	}
	if err := dc.load(); err != nil {
		fmt.Fprintf(os.Stderr, "disk cache: %v\n", err)
	}
	return dc
}

func (dc *diskCache) load() error {
	f, err := os.Open(dc.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	var d diskData
	if err := json.NewDecoder(f).Decode(&d); err != nil {
		return fmt.Errorf("corrupt cache, starting fresh: %w", err)
	}
	if d.AllClicks == nil {
		d.AllClicks = map[string]int{}
	}
	if d.IssueClicks == nil {
		d.IssueClicks = map[string]issueEntry{}
	}
	dc.d = d
	return nil
}

// persistLocked writes to disk. Caller must hold dc.mu.
func (dc *diskCache) persistLocked() {
	if err := os.MkdirAll(filepath.Dir(dc.path), 0700); err != nil {
		return
	}
	tmp := dc.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	if err := json.NewEncoder(f).Encode(dc.d); err != nil {
		f.Close()
		os.Remove(tmp)
		return
	}
	f.Close()
	os.Rename(tmp, dc.path)
}

func (dc *diskCache) allClicksFresh() bool {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return len(dc.d.AllClicks) > 0 && time.Since(dc.d.AllSaved) < allClicksTTL
}

func (dc *diskCache) getAllClicks() map[string]int {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.d.AllClicks
}

func (dc *diskCache) setAllClicks(counts map[string]int) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.d.AllClicks = counts
	dc.d.AllSaved = time.Now()
	dc.persistLocked()
}

func (dc *diskCache) getIssueClicks(emailID string) (map[string]int, bool) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	e, ok := dc.d.IssueClicks[emailID]
	if !ok || time.Since(e.Saved) > issueClicksTTL {
		return nil, false
	}
	return e.Clicks, true
}

func (dc *diskCache) setIssueClicks(emailID string, counts map[string]int) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.d.IssueClicks[emailID] = issueEntry{Clicks: counts, Saved: time.Now()}
	dc.persistLocked()
}
