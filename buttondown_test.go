package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeEvent(url string) emailEvent {
	return emailEvent{Metadata: map[string]string{"url": url}}
}

func TestFetchAllClicks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(eventsPage{
			Results: []emailEvent{fakeEvent("https://a.com"), fakeEvent("https://a.com"), fakeEvent("https://b.com")},
			Count:   3,
		})
	}))
	defer srv.Close()
	buttondownBase = srv.URL

	counts, err := fetchAllClicks("key")
	if err != nil {
		t.Fatal(err)
	}
	if counts["https://a.com"] != 2 {
		t.Errorf("a.com: got %d want 2", counts["https://a.com"])
	}
	if counts["https://b.com"] != 1 {
		t.Errorf("b.com: got %d want 1", counts["https://b.com"])
	}
}

func TestFetchClicksForEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("email_id") != "abc-123" {
			http.Error(w, "wrong email_id", 400)
			return
		}
		json.NewEncoder(w).Encode(eventsPage{
			Results: []emailEvent{fakeEvent("https://x.com"), fakeEvent("https://x.com")},
		})
	}))
	defer srv.Close()
	buttondownBase = srv.URL

	counts, err := fetchClicksForEmail("key", "abc-123")
	if err != nil {
		t.Fatal(err)
	}
	if counts["https://x.com"] != 2 {
		t.Errorf("x.com: got %d want 2", counts["https://x.com"])
	}
}

func TestLookupEmailByIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("subject") != "322" {
			json.NewEncoder(w).Encode(emailsPage{})
			return
		}
		json.NewEncoder(w).Encode(emailsPage{
			Results: []email{{ID: "uuid-1", Subject: "DevOps'ish 322"}},
		})
	}))
	defer srv.Close()
	buttondownBase = srv.URL

	id, subject, err := lookupEmailByIssue("key", 322)
	if err != nil {
		t.Fatal(err)
	}
	if id != "uuid-1" || subject != "DevOps'ish 322" {
		t.Errorf("got id=%q subject=%q", id, subject)
	}
}

func TestLookupEmailByIssueNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(emailsPage{})
	}))
	defer srv.Close()
	buttondownBase = srv.URL

	_, _, err := lookupEmailByIssue("key", 999)
	if err == nil {
		t.Error("expected error for unknown issue")
	}
}

func TestIssueNumberFromSubject(t *testing.T) {
	cases := []struct {
		subject string
		want    int
	}{
		{"DevOps'ish 322", 322},
		{"DevOps'ish 1", 1},
		{"DevOps'ish 296: OpenClaw flaws, Notepad++ hit, and Ingress NGINX CVEs", 296},
		{"DevOps'ish 299: Anthropic vs. US Gov, something else", 299},
		{"DevOps'ish Returns", 0},
		{"Sponsor DevOps’ish", 0},
		{"Terms of Service", 0},
		{"no number here", 0},
	}
	for _, c := range cases {
		got := issueNumberFromSubject(c.subject)
		if got != c.want {
			t.Errorf("issueNumberFromSubject(%q) = %d, want %d", c.subject, got, c.want)
		}
	}
}
