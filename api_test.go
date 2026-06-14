package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		switch r.URL.Path {
		case "/emails":
			json.NewEncoder(w).Encode(emailsPage{
				Results: []email{
					{ID: "id-1", Subject: "DevOps'ish 322"},
					{ID: "id-2", Subject: "DevOps'ish 321"},
				},
			})
		case "/events":
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
		t.Errorf("Issues len: got %d want 2", len(resp.Issues))
	}
	if resp.Issues[0].Number != 322 {
		t.Errorf("Issues[0].Number: got %d want 322", resp.Issues[0].Number)
	}
	if resp.Issues[0].TotalClicks != 1 {
		t.Errorf("Issues[0].TotalClicks: got %d want 1", resp.Issues[0].TotalClicks)
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

func TestHandleBottomLinks(t *testing.T) {
	results := append(
		fakeEvents("https://popular.com/page", 50),
		fakeEvents("https://unpopular.com/page", 1)...,
	)
	cleanup := fakeButtondown(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(eventsPage{Results: results})
	})
	defer cleanup()

	s := newServer("key", "Test")
	req := httptest.NewRequest("GET", "/api/links/bottom", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200", w.Code)
	}
	var resp linksResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Links) != 2 {
		t.Errorf("Links len: got %d want 2", len(resp.Links))
	}
	// sorted ascending: unpopular first
	if resp.Links[0].URL != "https://unpopular.com/page" {
		t.Errorf("Links[0].URL: got %q want unpopular", resp.Links[0].URL)
	}
	if resp.Links[0].Clicks != 1 {
		t.Errorf("Links[0].Clicks: got %d want 1", resp.Links[0].Clicks)
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
			json.NewEncoder(w).Encode(analytics{Recipients: 1000})
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
