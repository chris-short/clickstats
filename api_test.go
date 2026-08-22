package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeButtondown starts a mock Buttondown server and points buttondownBase at it.
// Call the returned func to shut it down.
func fakeButtondown(handler http.HandlerFunc) func() {
	srv := httptest.NewServer(handler)
	buttondownBase = srv.URL
	return srv.Close
}

func TestServerServesIndex(t *testing.T) {
	s := newServer("dummy", "Test")
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /: got %d, want 200", w.Code)
	}
}

func TestHandleStats(t *testing.T) {
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events":
			json.NewEncoder(w).Encode(eventsPage{
				Results: []emailEvent{fakeEvent("https://a.com"), fakeEvent("https://a.com"), fakeEvent("https://b.com")},
				Count:   3,
			})
		case "/emails":
			json.NewEncoder(w).Encode(emailsPage{Count: 5})
		}
	})
	defer cleanup()

	s := newServer("key", "Test")
	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	var resp statsResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.TotalClicks != 3 {
		t.Errorf("TotalClicks: got %d want 3", resp.TotalClicks)
	}
	if resp.IssuesSent != 5 {
		t.Errorf("IssuesSent: got %d want 5", resp.IssuesSent)
	}
	if len(resp.TopLinks) != 2 {
		t.Errorf("TopLinks len: got %d want 2", len(resp.TopLinks))
	}
	if resp.TopLinks[0].URL != "https://a.com" || resp.TopLinks[0].Clicks != 2 {
		t.Errorf("TopLinks[0]: %+v", resp.TopLinks[0])
	}
}

func TestHandleIssueStats(t *testing.T) {
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/emails":
			json.NewEncoder(w).Encode(emailsPage{
				Results: []email{{ID: "uid-1", Subject: "DevOps'ish 322"}},
			})
		case "/events":
			json.NewEncoder(w).Encode(eventsPage{
				Results: []emailEvent{fakeEvent("https://x.com"), fakeEvent("https://x.com")},
			})
		}
	})
	defer cleanup()

	s := newServer("key", "Test")
	req := httptest.NewRequest("GET", "/api/stats/issue/322", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	var resp issueStatsResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Issue != 322 {
		t.Errorf("Issue: got %d want 322", resp.Issue)
	}
	if resp.TotalClicks != 2 {
		t.Errorf("TotalClicks: got %d want 2", resp.TotalClicks)
	}
}

func TestHandleIssueStatsNotFound(t *testing.T) {
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(emailsPage{})
	})
	defer cleanup()

	s := newServer("key", "Test")
	req := httptest.NewRequest("GET", "/api/stats/issue/999", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d want 404", w.Code)
	}
}

func TestHandleIssues(t *testing.T) {
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/analytics"):
			json.NewEncoder(w).Encode(analytics{Deliveries: 200, Opens: 100, Clicks: 20})
		case r.URL.Path == "/emails":
			json.NewEncoder(w).Encode(emailsPage{
				Results: []email{
					{ID: "id-1", Subject: "DevOps'ish 322", PublishDate: "2026-08-07T12:00:00Z"},
					{ID: "id-2", Subject: "DevOps'ish 321"},
				},
			})
		case r.URL.Path == "/events":
			json.NewEncoder(w).Encode(eventsPage{
				Results: []emailEvent{fakeEvent("https://a.com")},
			})
		}
	})
	defer cleanup()

	s := newServer("key", "Test")
	req := httptest.NewRequest("GET", "/api/issues", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	var resp issuesResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Issues) != 2 {
		t.Fatalf("Issues len: got %d want 2", len(resp.Issues))
	}
	got := resp.Issues[0]
	if got.Number != 322 {
		t.Errorf("Number: got %d want 322", got.Number)
	}
	if got.TotalClicks != 1 {
		t.Errorf("TotalClicks: got %d want 1", got.TotalClicks)
	}
	if got.Date != "2026-08-07T12:00:00Z" {
		t.Errorf("Date: got %q want \"2026-08-07T12:00:00Z\"", got.Date)
	}
	if got.Deliveries != 200 || got.Opens != 100 {
		t.Errorf("Deliveries/Opens: got %d/%d want 200/100", got.Deliveries, got.Opens)
	}
	if got.OpenRate != 50 {
		t.Errorf("OpenRate: got %v want 50", got.OpenRate)
	}
	if got.ClickRate != 10 {
		t.Errorf("ClickRate: got %v want 10", got.ClickRate)
	}
}

// Clicks on excluded domains are dropped from the issue total so it matches the
// link list the dashboard shows for that issue.
func TestHandleIssuesExcludesDomains(t *testing.T) {
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/analytics"):
			json.NewEncoder(w).Encode(analytics{Deliveries: 100, Opens: 40, Clicks: 5})
		case r.URL.Path == "/emails":
			json.NewEncoder(w).Encode(emailsPage{
				Results: []email{{ID: "id-1", Subject: "DevOps'ish 322"}},
			})
		case r.URL.Path == "/events":
			json.NewEncoder(w).Encode(eventsPage{Results: []emailEvent{
				fakeEvent("https://a.com"),
				fakeEvent("https://buttondown.com/unsubscribe"),
			}})
		}
	})
	defer cleanup()

	s := newServer("key", "Test")
	s.excludeDomains = map[string]bool{"buttondown.com": true}
	req := httptest.NewRequest("GET", "/api/issues", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	var resp issuesResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Issues) != 1 {
		t.Fatalf("Issues len: got %d want 1", len(resp.Issues))
	}
	if resp.Issues[0].TotalClicks != 1 {
		t.Errorf("TotalClicks: got %d want 1 (buttondown.com excluded)", resp.Issues[0].TotalClicks)
	}
}

// A failing analytics call must not sink the whole issues response; clicks
// still come through and Deliveries stays 0 to mark the rates as unknown.
func TestHandleIssuesAnalyticsFailure(t *testing.T) {
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/analytics"):
			http.Error(w, "boom", http.StatusInternalServerError)
		case r.URL.Path == "/emails":
			json.NewEncoder(w).Encode(emailsPage{
				Results: []email{{ID: "id-1", Subject: "DevOps'ish 322"}},
			})
		case r.URL.Path == "/events":
			json.NewEncoder(w).Encode(eventsPage{Results: []emailEvent{fakeEvent("https://a.com")}})
		}
	})
	defer cleanup()

	s := newServer("key", "Test")
	req := httptest.NewRequest("GET", "/api/issues", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	var resp issuesResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Issues) != 1 {
		t.Fatalf("Issues len: got %d want 1", len(resp.Issues))
	}
	if resp.Issues[0].TotalClicks != 1 {
		t.Errorf("TotalClicks: got %d want 1", resp.Issues[0].TotalClicks)
	}
	if resp.Issues[0].Deliveries != 0 || resp.Issues[0].OpenRate != 0 {
		t.Errorf("want zeroed analytics, got %+v", resp.Issues[0])
	}
}

func fakeEvents(url string, n int) []emailEvent {
	evts := make([]emailEvent, n)
	for i := range evts {
		evts[i] = fakeEvent(url)
	}
	return evts
}

func TestHandleDomains(t *testing.T) {
	// a.com gets 101 clicks (above threshold), b.com gets 50 (below threshold).
	results := append(fakeEvents("https://a.com/page1", 101), fakeEvents("https://b.com/page1", 50)...)
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(eventsPage{Results: results})
	})
	defer cleanup()

	s := newServer("key", "Test")
	req := httptest.NewRequest("GET", "/api/domains", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	var resp domainsResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Domains) != 1 {
		t.Errorf("Domains len: got %d want 1 (only a.com >= 100 clicks)", len(resp.Domains))
	}
	if len(resp.Domains) > 0 {
		if resp.Domains[0].Domain != "a.com" {
			t.Errorf("Domains[0].Domain: got %q want \"a.com\"", resp.Domains[0].Domain)
		}
		if resp.Domains[0].Clicks != 101 {
			t.Errorf("Domains[0].Clicks: got %d want 101", resp.Domains[0].Clicks)
		}
	}
}

func TestHandleBottomDomains(t *testing.T) {
	results := append(
		fakeEvents("https://popular.com/page", 50),
		fakeEvents("https://unpopular.com/page", 1)...,
	)
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(eventsPage{Results: results})
	})
	defer cleanup()

	s := newServer("key", "Test")
	req := httptest.NewRequest("GET", "/api/domains/bottom", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	var resp domainsResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Domains) != 2 {
		t.Errorf("Domains len: got %d want 2", len(resp.Domains))
	}
	// sorted ascending: unpopular first
	if resp.Domains[0].Domain != "unpopular.com" {
		t.Errorf("Domains[0].Domain: got %q want \"unpopular.com\"", resp.Domains[0].Domain)
	}
	if resp.Domains[0].Clicks != 1 {
		t.Errorf("Domains[0].Clicks: got %d want 1", resp.Domains[0].Clicks)
	}
}

func TestHandleDomainLinks(t *testing.T) {
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(eventsPage{
			Results: []emailEvent{
				fakeEvent("https://a.com/page1"),
				fakeEvent("https://a.com/page2"),
				fakeEvent("https://a.com/page1"),
				fakeEvent("https://b.com/page1"),
			},
		})
	})
	defer cleanup()

	s := newServer("key", "Test")
	req := httptest.NewRequest("GET", "/api/domains/a.com", nil)
	req.SetPathValue("domain", "a.com")
	w := httptest.NewRecorder()
	s.handleDomainLinks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	var resp domainLinksResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Domain != "a.com" {
		t.Errorf("Domain: got %q want \"a.com\"", resp.Domain)
	}
	if len(resp.Links) != 2 {
		t.Errorf("Links len: got %d want 2", len(resp.Links))
	}
	if resp.Links[0].URL != "https://a.com/page1" || resp.Links[0].Clicks != 2 {
		t.Errorf("Links[0]: %+v", resp.Links[0])
	}
}

func TestHandleTrends(t *testing.T) {
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/emails":
			json.NewEncoder(w).Encode(emailsPage{
				Results: []email{
					{ID: "id-1", Subject: "DevOps'ish 321", PublishDate: "2024-01-08"},
					{ID: "id-2", Subject: "DevOps'ish 322", PublishDate: "2024-01-15"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/analytics"):
			json.NewEncoder(w).Encode(analytics{Deliveries: 1000, Opens: 450, Clicks: 120})
		}
	})
	defer cleanup()

	s := newServer("key", "Test")
	req := httptest.NewRequest("GET", "/api/trends", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	var resp trendsResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Points) != 2 {
		t.Errorf("Points len: got %d want 2", len(resp.Points))
	}
	if resp.Points[0].Issue != 321 {
		t.Errorf("Points[0].Issue: got %d want 321", resp.Points[0].Issue)
	}
	if resp.Points[0].OpenRate != 45.0 {
		t.Errorf("Points[0].OpenRate: got %f want 45.0", resp.Points[0].OpenRate)
	}
}

func TestRefreshAllPopulatesCache(t *testing.T) {
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events":
			json.NewEncoder(w).Encode(eventsPage{
				Results: []emailEvent{fakeEvent("https://a.com"), fakeEvent("https://a.com"), fakeEvent("https://b.com")},
				Count:   3,
			})
		case "/emails":
			json.NewEncoder(w).Encode(emailsPage{
				Count:   5,
				Results: []email{{ID: "id-1", Subject: "DevOps'ish 322"}},
			})
		}
	})
	defer cleanup()

	s := newServer("key", "Test")
	s.refreshAll()

	for _, key := range []string{"stats", "domains", "issues"} {
		if _, ok := s.cache.get(key); !ok {
			t.Errorf("%q not cached after refreshAll", key)
		}
	}

	v, _ := s.cache.get("stats")
	resp, ok := v.(statsResponse)
	if !ok {
		t.Fatalf("stats cache entry has type %T, want statsResponse", v)
	}
	if resp.TotalClicks != 3 {
		t.Errorf("stats.TotalClicks: got %d want 3", resp.TotalClicks)
	}
}

func TestParsePublishDate(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"2026-08-07T12:00:00Z", false},
		{"2024-01-15", false},
		{"", true},
		{"not a date", true},
	}
	for _, c := range cases {
		_, err := parsePublishDate(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parsePublishDate(%q): err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
	}
}

func TestEngagementTTLFor(t *testing.T) {
	s := newServer("key", "Test")
	ago := func(d time.Duration) string { return time.Now().Add(-d).Format(time.RFC3339) }
	cases := []struct {
		name string
		date string
		want time.Duration
	}{
		{"just sent", ago(2 * time.Hour), engagementFreshTTL},
		{"still settling", ago(48 * time.Hour), engagementFreshTTL},
		{"past settle age", ago(5 * 24 * time.Hour), engagementWarmTTL},
		{"old issue", ago(90 * 24 * time.Hour), engagementColdTTL},
		// An unknown age must not be treated as old, or its numbers freeze.
		{"unparseable", "who knows", engagementFreshTTL},
	}
	for _, c := range cases {
		if got := s.engagementTTLFor(c.date); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

// A fast refresh interval must not be overridden by a longer analytics TTL.
func TestAnalyticsTTLRespectsRefreshInterval(t *testing.T) {
	s := newServer("key", "Test")
	s.refreshInterval = time.Minute
	if got := s.engagementTTLFor(time.Now().Format(time.RFC3339)); got != time.Minute {
		t.Errorf("got %s want 1m0s", got)
	}
	// A slower interval than the tier leaves the tier alone.
	s.refreshInterval = time.Hour
	if got := s.engagementTTLFor(time.Now().Format(time.RFC3339)); got != engagementFreshTTL {
		t.Errorf("got %s want %s", got, engagementFreshTTL)
	}
}

// Analytics for a recently sent issue must refetch even when a cached entry
// exists, because opens are still piling up.
func TestAnalyticsRefetchedForRecentIssue(t *testing.T) {
	calls := 0
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/analytics") {
			calls++
			json.NewEncoder(w).Encode(analytics{Deliveries: 100, Opens: 60, Clicks: 10})
		}
	})
	defer cleanup()

	s := newServer("key", "Test")
	s.disk = newDiskCache(t.TempDir())
	// Cached an hour ago: stale for a fresh issue, fine for an old one.
	s.disk.d.Analytics["id-1"] = analyticsEntry{
		A:     analytics{Deliveries: 100, Opens: 10},
		Saved: time.Now().Add(-time.Hour),
	}

	got, err := s.cachedEmailAnalytics("id-1", time.Now().Add(-2*time.Hour).Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("made %d analytics calls, want 1 (recent issue must refetch)", calls)
	}
	if got.Opens != 60 {
		t.Errorf("Opens: got %d want 60 (want the refetched value)", got.Opens)
	}
}

// The flip side: an issue that has stopped moving must be served from cache so
// the freshness gain doesn't cost an API call per issue per refresh.
func TestAnalyticsCachedForOldIssue(t *testing.T) {
	calls := 0
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/analytics") {
			calls++
			json.NewEncoder(w).Encode(analytics{Deliveries: 100, Opens: 60})
		}
	})
	defer cleanup()

	s := newServer("key", "Test")
	s.disk = newDiskCache(t.TempDir())
	s.disk.d.Analytics["id-1"] = analyticsEntry{
		A:     analytics{Deliveries: 100, Opens: 10},
		Saved: time.Now().Add(-time.Hour),
	}

	got, err := s.cachedEmailAnalytics("id-1", "2023-01-15")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("made %d analytics calls, want 0 (old issue should use cache)", calls)
	}
	if got.Opens != 10 {
		t.Errorf("Opens: got %d want 10 (cached value)", got.Opens)
	}
}

// Per-issue click breakdowns follow the same rule as analytics: a just-sent
// issue is still collecting clicks, so a cached breakdown goes stale quickly.
func TestIssueClicksRefetchedForRecentIssue(t *testing.T) {
	calls := 0
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/events" {
			calls++
			json.NewEncoder(w).Encode(eventsPage{Results: []emailEvent{
				fakeEvent("https://a.com"), fakeEvent("https://a.com"),
			}})
		}
	})
	defer cleanup()

	s := newServer("key", "Test")
	s.disk = newDiskCache(t.TempDir())
	s.disk.d.IssueClicks["id-1"] = issueEntry{
		Clicks: map[string]int{"https://a.com": 1},
		Saved:  time.Now().Add(-time.Hour),
	}

	recent, err := s.cachedClicksForEmail("id-1", time.Now().Add(-2*time.Hour).Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("made %d click calls, want 1 (recent issue must refetch)", calls)
	}
	if recent["https://a.com"] != 2 {
		t.Errorf("a.com: got %d want 2 (refetched value)", recent["https://a.com"])
	}

	// An issue that has stopped moving still comes from cache.
	calls = 0
	s.cache = newCache(defaultMemTTL)
	s.disk.d.IssueClicks["id-2"] = issueEntry{
		Clicks: map[string]int{"https://a.com": 1},
		Saved:  time.Now().Add(-time.Hour),
	}
	old, err := s.cachedClicksForEmail("id-2", "2023-01-15")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("made %d click calls, want 0 (old issue should use cache)", calls)
	}
	if old["https://a.com"] != 1 {
		t.Errorf("a.com: got %d want 1 (cached value)", old["https://a.com"])
	}
}

func clickEvent(url string, at time.Time) emailEvent {
	return emailEvent{CreationDate: at, Metadata: map[string]string{"url": url}}
}

// eventServer fakes the click event collection: newest-first, paged, and
// counting the all-clicks requests so an incremental sync is distinguishable
// from a full walk. Requests without the descending ordering are rejected,
// since the early stop is only correct when newer events come first.
type eventServer struct {
	events   []emailEvent // newest first
	pageSize int
	requests int
	base     string
	srv      *httptest.Server
}

func newEventServer(t *testing.T, pageSize int, events ...emailEvent) *eventServer {
	t.Helper()
	es := &eventServer{events: events, pageSize: pageSize}
	es.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case strings.HasSuffix(r.URL.Path, "/analytics"):
			json.NewEncoder(w).Encode(analytics{Deliveries: 10, Opens: 5})
		case r.URL.Path == "/emails":
			json.NewEncoder(w).Encode(emailsPage{Count: 1, Results: []email{{ID: "id-1", Subject: "DevOps'ish 322"}}})
		case r.URL.Path == "/events" && q.Get("email_id") != "":
			json.NewEncoder(w).Encode(eventsPage{Results: es.events})
		case r.URL.Path == "/events":
			if q.Get("ordering") != "-creation_date" {
				http.Error(w, "walk must request newest-first ordering", http.StatusBadRequest)
				return
			}
			es.requests++
			page, _ := strconv.Atoi(q.Get("page"))
			if page < 1 {
				page = 1
			}
			start := min((page-1)*es.pageSize, len(es.events))
			end := min(start+es.pageSize, len(es.events))
			out := eventsPage{Results: es.events[start:end], Count: len(es.events)}
			if end < len(es.events) {
				next := fmt.Sprintf("%s/events?event_type=clicked&ordering=-creation_date&page=%d", es.base, page+1)
				out.Next = &next
			}
			json.NewEncoder(w).Encode(out)
		}
	}))
	es.base = es.srv.URL
	buttondownBase = es.srv.URL
	t.Cleanup(es.srv.Close)
	return es
}

// prepend adds a newer event, the way a fresh click would arrive.
func (es *eventServer) prepend(e emailEvent) {
	es.events = append([]emailEvent{e}, es.events...)
}

// primedServer returns a server whose cache has been filled by a full walk.
func primedServer(t *testing.T, es *eventServer) *server {
	t.Helper()
	s := newServer("key", "Test")
	s.disk = newDiskCache(t.TempDir())
	s.refreshAll()
	es.requests = 0
	s.cache = newCache(defaultMemTTL) // force the disk path, as a later tick would
	return s
}

// The payoff: an unchanged history costs one request, not a re-download.
func TestRefreshUnchangedCostsOneRequest(t *testing.T) {
	now := time.Now()
	es := newEventServer(t, 2,
		clickEvent("https://a.com", now.Add(-time.Hour)),
		clickEvent("https://a.com", now.Add(-2*time.Hour)),
		clickEvent("https://b.com", now.Add(-3*time.Hour)),
	)
	s := primedServer(t, es)

	s.refreshAll()

	if es.requests != 1 {
		t.Errorf("made %d all-clicks requests, want 1 (nothing changed)", es.requests)
	}
	counts, _ := s.cache.get(rawClicksKey)
	if got := counts.(map[string]int)["https://a.com"]; got != 2 {
		t.Errorf("a.com: got %d want 2 (cache must survive a no-op refresh)", got)
	}
}

// New clicks are fetched without re-walking the history behind them, and merge
// into the existing counts rather than replacing or double counting them.
func TestRefreshSyncsOnlyNewClicks(t *testing.T) {
	now := time.Now()
	es := newEventServer(t, 2,
		clickEvent("https://a.com", now.Add(-time.Hour)),
		clickEvent("https://a.com", now.Add(-2*time.Hour)),
		clickEvent("https://b.com", now.Add(-3*time.Hour)),
	)
	s := primedServer(t, es)

	es.prepend(clickEvent("https://b.com", now))
	s.refreshAll()

	if es.requests != 1 {
		t.Errorf("made %d all-clicks requests, want 1 (delta fits in a page)", es.requests)
	}
	v, _ := s.cache.get(rawClicksKey)
	counts := v.(map[string]int)
	if counts["https://b.com"] != 2 {
		t.Errorf("b.com: got %d want 2 (1 cached + 1 new)", counts["https://b.com"])
	}
	if counts["https://a.com"] != 2 {
		t.Errorf("a.com: got %d want 2 (untouched by the delta)", counts["https://a.com"])
	}
}

// When the totals stop adding up, something changed behind the high-water mark
// and the sync must rebuild instead of trusting the cache.
func TestRefreshRebuildsOnDrift(t *testing.T) {
	now := time.Now()
	es := newEventServer(t, 2,
		clickEvent("https://a.com", now.Add(-time.Hour)),
		clickEvent("https://a.com", now.Add(-2*time.Hour)),
		clickEvent("https://b.com", now.Add(-3*time.Hour)),
	)
	s := primedServer(t, es)

	// An event disappears from behind the mark, as a deleted subscriber's would.
	es.events = es.events[:2]
	s.refreshAll()

	if es.requests < 2 {
		t.Errorf("made %d all-clicks requests, want a full rebuild", es.requests)
	}
	v, _ := s.cache.get(rawClicksKey)
	counts := v.(map[string]int)
	if _, ok := counts["https://b.com"]; ok {
		t.Errorf("b.com should be gone after the rebuild, got %d", counts["https://b.com"])
	}
	if counts["https://a.com"] != 2 {
		t.Errorf("a.com: got %d want 2", counts["https://a.com"])
	}
}

func TestHandlePrintNoSponsor(t *testing.T) {
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/emails":
			json.NewEncoder(w).Encode(emailsPage{
				Results: []email{{ID: "uid-1", Subject: "DevOps'ish 322"}},
			})
		case "/events":
			json.NewEncoder(w).Encode(eventsPage{
				Results: []emailEvent{fakeEvent("https://a.com")},
			})
		}
	})
	defer cleanup()

	s := newServer("key", "Test")
	req := httptest.NewRequest("GET", "/print/issue/322", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Issue #322") {
		t.Error("missing 'Issue #322'")
	}
	if !strings.Contains(body, "https://a.com") {
		t.Error("missing link URL")
	}
	if strings.Contains(body, "Sponsor Performance") {
		t.Error("should not show sponsor block when no sponsor param")
	}
}

func TestHandlePrintWithSponsor(t *testing.T) {
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/emails":
			json.NewEncoder(w).Encode(emailsPage{
				Results: []email{{ID: "uid-1", Subject: "DevOps'ish 322"}},
			})
		case "/events":
			json.NewEncoder(w).Encode(eventsPage{
				Results: []emailEvent{
					fakeEvent("https://sponsor.com/page"),
					fakeEvent("https://other.com"),
				},
			})
		case "/emails/uid-1/analytics":
			json.NewEncoder(w).Encode(analytics{Deliveries: 1000})
		}
	})
	defer cleanup()

	s := newServer("key", "Test")
	req := httptest.NewRequest("GET", "/print/issue/322?sponsor=https://sponsor.com/page", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Sponsor Performance") {
		t.Error("missing sponsor section")
	}
	if !strings.Contains(body, "0.10%") {
		t.Error("missing click rate (1/1000 = 0.10%)")
	}
	if !strings.Contains(body, "#1 of 2") {
		t.Error("missing rank")
	}
}
