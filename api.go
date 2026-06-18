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
	TotalLinks        int         `json:"total_links"`
	IssuesSent        int         `json:"issues_sent"`
	AvgClicksPerIssue int         `json:"avg_clicks_per_issue"`
	AvgLinksPerIssue  int         `json:"avg_links_per_issue"`
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

type domainLinksResponse struct {
	Domain string      `json:"domain"`
	Links  []linkCount `json:"links"`
}

type trendsDataPoint struct {
	Issue     int     `json:"issue"`
	Subject   string  `json:"subject"`
	Date      string  `json:"date"`
	OpenRate  float64 `json:"open_rate"`
	ClickRate float64 `json:"click_rate"`
}

type trendsResponse struct {
	Points []trendsDataPoint `json:"points"`
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

func (s *server) isExcluded(rawURL string) bool {
	return len(s.excludeDomains) > 0 && s.excludeDomains[extractDomain(rawURL)]
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

// cachedAllClicks returns all-time click counts. It checks the in-memory cache,
// then the disk cache, then falls back to the Buttondown API.
func (s *server) cachedAllClicks() (map[string]int, error) {
	const key = "_raw_clicks"
	if v, ok := s.cache.get(key); ok {
		return v.(map[string]int), nil
	}
	if s.disk != nil && s.disk.allClicksFresh() {
		counts := s.disk.getAllClicks()
		s.cache.set(key, counts)
		return counts, nil
	}
	counts, err := fetchAllClicks(s.apiKey)
	if err != nil {
		// Serve stale disk data rather than failing completely.
		if s.disk != nil {
			if stale := s.disk.getAllClicks(); len(stale) > 0 {
				fmt.Fprintf(os.Stderr, "API error, serving stale cache: %v\n", err)
				s.cache.set(key, stale)
				return stale, nil
			}
		}
		return nil, err
	}
	s.cache.set(key, counts)
	if s.disk != nil {
		s.disk.setAllClicks(counts)
	}
	return counts, nil
}

func (s *server) cachedEmailAnalytics(emailID string) (analytics, error) {
	memKey := "analytics:" + emailID
	if v, ok := s.cache.get(memKey); ok {
		return v.(analytics), nil
	}
	if s.disk != nil {
		if a, ok := s.disk.getAnalytics(emailID); ok {
			s.cache.set(memKey, a)
			return a, nil
		}
	}
	a, err := fetchEmailAnalytics(s.apiKey, emailID)
	if err != nil {
		return analytics{}, err
	}
	s.cache.set(memKey, a)
	if s.disk != nil {
		s.disk.setAnalytics(emailID, a)
	}
	return a, nil
}

// cachedClicksForEmail returns click counts for a single email, checking
// in-memory cache, then disk cache, before calling the API.
func (s *server) cachedClicksForEmail(emailID string) (map[string]int, error) {
	memKey := "email:" + emailID
	if v, ok := s.cache.get(memKey); ok {
		return v.(map[string]int), nil
	}
	if s.disk != nil {
		if counts, ok := s.disk.getIssueClicks(emailID); ok {
			s.cache.set(memKey, counts)
			return counts, nil
		}
	}
	counts, err := fetchClicksForEmail(s.apiKey, emailID)
	if err != nil {
		return nil, err
	}
	s.cache.set(memKey, counts)
	if s.disk != nil {
		s.disk.setIssueClicks(emailID, counts)
	}
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

	total      := sumCounts(counts)
	totalLinks := len(counts)
	avgClicks, avgLinks := 0, 0
	if issueCount > 0 {
		avgClicks = total / issueCount
		avgLinks  = totalLinks / issueCount
	}
	return statsResponse{
		TotalClicks:       total,
		TotalLinks:        totalLinks,
		IssuesSent:        issueCount,
		AvgClicksPerIssue: avgClicks,
		AvgLinksPerIssue:  avgLinks,
		TopLinks:          sortedLinks(counts, 50),
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
		if d == "" || s.excludeDomains[d] {
			continue
		}
		domainClicks[d] += c
		domainLinks[d]++
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
	var result []domainCount
	for _, d := range domains {
		if d.Clicks >= 100 {
			result = append(result, d)
		}
	}
	return domainsResponse{Domains: result}, nil
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
			counts, err := s.cachedClicksForEmail(e.ID)
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

func (s *server) handleTrends(w http.ResponseWriter, r *http.Request) {
	const cacheKey = "trends"
	if v, ok := s.cache.get(cacheKey); ok {
		writeJSON(w, v)
		return
	}
	emails, err := fetchAllNewsletterEmails(s.apiKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var newsletters []email
	for _, e := range emails {
		if issueNumberFromSubject(e.Subject) > 0 {
			newsletters = append(newsletters, e)
		}
	}

	type result struct {
		point trendsDataPoint
		ok    bool
	}
	results := make([]result, len(newsletters))
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	for i, e := range newsletters {
		wg.Add(1)
		go func(i int, e email) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			a, err := s.cachedEmailAnalytics(e.ID)
			if err != nil {
				return
			}
			results[i] = result{ok: true, point: trendsDataPoint{
				Issue:     issueNumberFromSubject(e.Subject),
				Subject:   e.Subject,
				Date:      e.PublishDate,
				OpenRate:  a.OpenRate,
				ClickRate: a.ClickRate,
			}}
		}(i, e)
	}
	wg.Wait()

	points := make([]trendsDataPoint, 0, len(results))
	for _, r := range results {
		if r.ok && r.point.Issue > 0 {
			points = append(points, r.point)
		}
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].Issue < points[j].Issue
	})
	resp := trendsResponse{Points: points}
	s.cache.set(cacheKey, resp)
	writeJSON(w, resp)
}

// handleDebugAnalytics fetches the raw analytics JSON from Buttondown for one
// email so we can verify the actual field names. Remove once confirmed.
func (s *server) handleDebugAnalytics(w http.ResponseWriter, r *http.Request) {
	emails, err := fetchRecentEmails(s.apiKey, 1)
	if err != nil || len(emails) == 0 {
		http.Error(w, "could not fetch emails: "+err.Error(), http.StatusInternalServerError)
		return
	}
	e := emails[0]
	url := fmt.Sprintf("%s/emails/%s/analytics", buttondownBase, e.ID)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Token "+s.apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	var raw json.RawMessage
	json.NewDecoder(resp.Body).Decode(&raw)
	w.Header().Set("Content-Type", "application/json")
	out, _ := json.MarshalIndent(map[string]interface{}{
		"email_id":      e.ID,
		"email_subject": e.Subject,
		"raw_analytics": raw,
	}, "", "  ")
	w.Write(out)
}

func (s *server) handleBottomDomains(w http.ResponseWriter, r *http.Request) {
	const cacheKey = "bottom_domains"
	if v, ok := s.cache.get(cacheKey); ok {
		writeJSON(w, v)
		return
	}
	counts, err := s.cachedAllClicks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	domainClicks := map[string]int{}
	domainLinks := map[string]int{}
	for u, c := range counts {
		d := extractDomain(u)
		if d == "" || s.excludeDomains[d] {
			continue
		}
		domainClicks[d] += c
		domainLinks[d]++
	}
	domains := make([]domainCount, 0, len(domainClicks))
	for d, c := range domainClicks {
		domains = append(domains, domainCount{Domain: d, Clicks: c, Links: domainLinks[d]})
	}
	sort.Slice(domains, func(i, j int) bool {
		if domains[i].Clicks != domains[j].Clicks {
			return domains[i].Clicks < domains[j].Clicks
		}
		return domains[i].Domain < domains[j].Domain
	})
	if len(domains) > 50 {
		domains = domains[:50]
	}
	resp := domainsResponse{Domains: domains}
	s.cache.set(cacheKey, resp)
	writeJSON(w, resp)
}

func (s *server) handleDomainLinks(w http.ResponseWriter, r *http.Request) {
	domain := r.PathValue("domain")
	counts, err := s.cachedAllClicks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var links []linkCount
	for u, c := range counts {
		if extractDomain(u) == domain {
			links = append(links, linkCount{URL: u, Clicks: c})
		}
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Clicks != links[j].Clicks {
			return links[i].Clicks > links[j].Clicks
		}
		return links[i].URL < links[j].URL
	})
	writeJSON(w, domainLinksResponse{Domain: domain, Links: links})
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
	counts, err := s.cachedClicksForEmail(emailID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := issueStatsResponse{
		Issue:       n,
		Subject:     subject,
		EmailID:     emailID,
		TotalClicks: sumCounts(counts),
		Links:       sortedLinks(counts, 0),
	}
	s.cache.set(cacheKey, resp)
	writeJSON(w, resp)
}
