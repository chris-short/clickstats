package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
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

type domainCount struct {
	Domain string `json:"domain"`
	Clicks int    `json:"clicks"`
	Links  int    `json:"links"`
}

type domainsResponse struct {
	Domains []domainCount `json:"domains"`
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

func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Host, "www.")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "writeJSON: %v\n", err)
	}
}

// --- Data loaders (shared by handlers and cache warmer) ---

// cachedAllClicks returns all-time click counts, using the cache to avoid
// redundant Buttondown paginations when both stats and domains need this data.
func (s *server) cachedAllClicks() (map[string]int, error) {
	const key = "_raw_clicks"
	if v, ok := s.cache.get(key); ok {
		return v.(map[string]int), nil
	}
	counts, err := fetchAllClicks(s.apiKey)
	if err != nil {
		return nil, err
	}
	s.cache.set(key, counts)
	return counts, nil
}

func (s *server) loadStats() (statsResponse, error) {
	var counts map[string]int
	var issueCount int
	var countsErr, emailErr error

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		counts, countsErr = s.cachedAllClicks()
	}()
	go func() {
		defer wg.Done()
		issueCount, emailErr = fetchEmailCount(s.apiKey)
	}()
	wg.Wait()

	if countsErr != nil {
		return statsResponse{}, countsErr
	}
	if emailErr != nil {
		return statsResponse{}, emailErr
	}

	total := sumCounts(counts)
	avg := 0
	if issueCount > 0 {
		avg = total / issueCount
	}
	return statsResponse{
		TotalClicks:       total,
		IssuesSent:        issueCount,
		AvgClicksPerIssue: avg,
		TopLinks:          sortedLinks(counts, 20),
	}, nil
}

func (s *server) loadDomains() (domainsResponse, error) {
	counts, err := s.cachedAllClicks()
	if err != nil {
		return domainsResponse{}, err
	}
	domainClicks := map[string]int{}
	domainLinks := map[string]int{}
	for u, c := range counts {
		d := extractDomain(u)
		if d != "" {
			domainClicks[d] += c
			domainLinks[d]++
		}
	}
	domains := make([]domainCount, 0, len(domainClicks))
	for d, c := range domainClicks {
		domains = append(domains, domainCount{Domain: d, Clicks: c, Links: domainLinks[d]})
	}
	sort.Slice(domains, func(i, j int) bool {
		if domains[i].Clicks != domains[j].Clicks {
			return domains[i].Clicks > domains[j].Clicks
		}
		return domains[i].Domain < domains[j].Domain
	})
	if len(domains) > 50 {
		domains = domains[:50]
	}
	return domainsResponse{Domains: domains}, nil
}

func (s *server) loadIssues() (issuesResponse, error) {
	all, err := fetchRecentEmails(s.apiKey, 100)
	if err != nil {
		return issuesResponse{}, err
	}
	emails := make([]email, 0, 10)
	for _, e := range all {
		if issueNumberFromSubject(e.Subject) > 0 {
			emails = append(emails, e)
			if len(emails) == 10 {
				break
			}
		}
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
			return issuesResponse{}, res.err
		}
		summaries = append(summaries, res.summary)
	}
	return issuesResponse{Issues: summaries}, nil
}

// warmCache fetches raw click data once, then derives stats and domains from
// the cached result so Buttondown is only paginated once at startup.
func (s *server) warmCache() {
	go func() {
		if _, err := s.cachedAllClicks(); err != nil {
			return
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if resp, err := s.loadStats(); err == nil {
				s.cache.set("stats", resp)
			}
		}()
		go func() {
			defer wg.Done()
			if resp, err := s.loadDomains(); err == nil {
				s.cache.set("domains", resp)
			}
		}()
		wg.Wait()
	}()
	go func() {
		if resp, err := s.loadIssues(); err == nil {
			s.cache.set("issues", resp)
		}
	}()
}

// --- Handlers ---

func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	const cacheKey = "stats"
	if v, ok := s.cache.get(cacheKey); ok {
		writeJSON(w, v)
		return
	}
	resp, err := s.loadStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
	resp, err := s.loadIssues()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cache.set(cacheKey, resp)
	writeJSON(w, resp)
}

func (s *server) handleDomains(w http.ResponseWriter, r *http.Request) {
	const cacheKey = "domains"
	if v, ok := s.cache.get(cacheKey); ok {
		writeJSON(w, v)
		return
	}
	resp, err := s.loadDomains()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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
