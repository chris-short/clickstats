package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
)

// --- Response types ---

type linkCount struct {
	URL    string `json:"url"`
	Clicks int    `json:"clicks"`
}

type statsResponse struct {
	TotalClicks       int         `json:"total_clicks"`
	IssuesSent        int         `json:"issues_sent"`
	AvgClicksPerIssue int         `json:"avg_clicks_per_issue"`
	TopLinks          []linkCount `json:"top_links"`
}

type issueSummary struct {
	Number      int    `json:"number"`
	EmailID     string `json:"email_id"`
	Subject     string `json:"subject"`
	TotalClicks int    `json:"total_clicks"`
}

type issuesResponse struct {
	Issues []issueSummary `json:"issues"`
}

type issueStatsResponse struct {
	Issue       int         `json:"issue"`
	Subject     string      `json:"subject"`
	EmailID     string      `json:"email_id"`
	TotalClicks int         `json:"total_clicks"`
	Links       []linkCount `json:"links"`
}

// --- Helpers ---

func sortedLinks(counts map[string]int, limit int) []linkCount {
	links := make([]linkCount, 0, len(counts))
	for u, c := range counts {
		links = append(links, linkCount{URL: u, Clicks: c})
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Clicks != links[j].Clicks {
			return links[i].Clicks > links[j].Clicks
		}
		return links[i].URL < links[j].URL
	})
	if limit > 0 && len(links) > limit {
		links = links[:limit]
	}
	return links
}

func sumCounts(counts map[string]int) int {
	n := 0
	for _, c := range counts {
		n += c
	}
	return n
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "writeJSON: %v\n", err)
	}
}

// --- Handlers ---

func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	const cacheKey = "stats"
	if v, ok := s.cache.get(cacheKey); ok {
		writeJSON(w, v)
		return
	}
	counts, err := fetchAllClicks(s.apiKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	issueCount, err := fetchEmailCount(s.apiKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	total := sumCounts(counts)
	avg := 0
	if issueCount > 0 {
		avg = total / issueCount
	}
	resp := statsResponse{
		TotalClicks:       total,
		IssuesSent:        issueCount,
		AvgClicksPerIssue: avg,
		TopLinks:          sortedLinks(counts, 20),
	}
	s.cache.set(cacheKey, resp)
	writeJSON(w, resp)
}

func (s *server) handleIssues(w http.ResponseWriter, r *http.Request) {
	const cacheKey = "issues"
	if v, ok := s.cache.get(cacheKey); ok {
		writeJSON(w, v)
		return
	}
	emails, err := fetchRecentEmails(s.apiKey, 10)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type result struct {
		summary issueSummary
		err     error
	}
	results := make([]result, len(emails))
	var wg sync.WaitGroup
	for i, e := range emails {
		wg.Add(1)
		go func(i int, e email) {
			defer wg.Done()
			counts, err := fetchClicksForEmail(s.apiKey, e.ID)
			if err != nil {
				results[i] = result{err: err}
				return
			}
			results[i] = result{summary: issueSummary{
				Number:      issueNumberFromSubject(e.Subject),
				EmailID:     e.ID,
				Subject:     e.Subject,
				TotalClicks: sumCounts(counts),
			}}
		}(i, e)
	}
	wg.Wait()

	summaries := make([]issueSummary, 0, len(results))
	for _, res := range results {
		if res.err != nil {
			http.Error(w, res.err.Error(), http.StatusInternalServerError)
			return
		}
		summaries = append(summaries, res.summary)
	}
	resp := issuesResponse{Issues: summaries}
	s.cache.set(cacheKey, resp)
	writeJSON(w, resp)
}

func (s *server) handleIssueStats(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		http.Error(w, "invalid issue number", http.StatusBadRequest)
		return
	}
	cacheKey := fmt.Sprintf("issue:%d", n)
	if v, ok := s.cache.get(cacheKey); ok {
		writeJSON(w, v)
		return
	}
	emailID, subject, err := lookupEmailByIssue(s.apiKey, n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	counts, err := fetchClicksForEmail(s.apiKey, emailID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := issueStatsResponse{
		Issue:       n,
		Subject:     subject,
		EmailID:     emailID,
		TotalClicks: sumCounts(counts),
		Links:       sortedLinks(counts, 20),
	}
	s.cache.set(cacheKey, resp)
	writeJSON(w, resp)
}
