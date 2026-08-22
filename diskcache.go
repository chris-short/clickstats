package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const allClicksTTL = time.Hour

type issueEntry struct {
	Clicks map[string]int `json:"c"`
	Saved  time.Time      `json:"s"`
}

type analyticsEntry struct {
	A     analytics `json:"a"`
	Saved time.Time `json:"s"`
}

type diskData struct {
	AllClicks map[string]int `json:"ac,omitempty"`
	AllSaved  time.Time      `json:"as,omitempty"`
	// AllTotal is the number of click events AllClicks was built from, as
	// reported by the events collection, and AllNewest is the timestamp of the
	// most recent event counted. Together they let a refresh fetch only what
	// arrived since, then check the result adds up to what the API reports.
	AllTotal    int                       `json:"at,omitempty"`
	AllNewest   time.Time                 `json:"an_newest,omitempty"`
	IssueClicks map[string]issueEntry     `json:"ic,omitempty"`
	Analytics   map[string]analyticsEntry `json:"an,omitempty"`
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
			Analytics:   map[string]analyticsEntry{},
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
	if d.Analytics == nil {
		d.Analytics = map[string]analyticsEntry{}
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

func (dc *diskCache) setAllClicks(counts map[string]int, total int, newest time.Time) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.d.AllClicks = counts
	dc.d.AllTotal = total
	dc.d.AllNewest = newest
	dc.d.AllSaved = time.Now()
	dc.persistLocked()
}

// allClicksState returns the cached clicks together with the high-water mark
// and event total they were built from. ok is false when there is nothing to
// sync against, including cache files written before the mark was recorded, so
// those fall back to one full walk and pick the mark up from there.
func (dc *diskCache) allClicksState() (counts map[string]int, newest time.Time, total int, ok bool) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if len(dc.d.AllClicks) == 0 || dc.d.AllNewest.IsZero() {
		return nil, time.Time{}, 0, false
	}
	return dc.d.AllClicks, dc.d.AllNewest, dc.d.AllTotal, true
}

// markAllClicksVerified records that the cached clicks were just confirmed
// current. Freshness here means "checked against the source", not "recently
// downloaded", so a confirmed-unchanged cache resets the TTL without refetching.
func (dc *diskCache) markAllClicksVerified() {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.d.AllSaved = time.Now()
	dc.persistLocked()
}

// getIssueClicks takes its TTL from the caller for the same reason
// getAnalytics does: a just-sent issue is still accumulating clicks, while an
// old one has stopped. See engagementTTLFor.
func (dc *diskCache) getIssueClicks(emailID string, ttl time.Duration) (map[string]int, bool) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	e, ok := dc.d.IssueClicks[emailID]
	if !ok || time.Since(e.Saved) > ttl {
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

// getAnalytics takes the TTL from the caller rather than using a fixed one: how
// long an issue's analytics stay valid depends on how long ago it was sent, and
// the disk cache doesn't know publish dates. See analyticsTTLFor.
func (dc *diskCache) getAnalytics(emailID string, ttl time.Duration) (analytics, bool) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	e, ok := dc.d.Analytics[emailID]
	if !ok || time.Since(e.Saved) > ttl {
		return analytics{}, false
	}
	return e.A, true
}

func (dc *diskCache) setAnalytics(emailID string, a analytics) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if dc.d.Analytics == nil {
		dc.d.Analytics = map[string]analyticsEntry{}
	}
	dc.d.Analytics[emailID] = analyticsEntry{A: a, Saved: time.Now()}
	dc.persistLocked()
}
