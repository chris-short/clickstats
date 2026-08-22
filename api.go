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
	"time"
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

// issueSummary carries per-issue engagement. TotalClicks counts click events
// (the same number the issue link breakdown adds up to), while Opens and the
// rates come from Buttondown's analytics endpoint. Deliveries is 0 when that
// analytics call failed, which is how the dashboard knows the rates are unknown
// rather than genuinely zero.
type issueSummary struct {
	Number      int     `json:"number"`
	EmailID     string  `json:"email_id"`
	Subject     string  `json:"subject"`
	Date        string  `json:"date"`
	TotalClicks int     `json:"total_clicks"`
	Deliveries  int     `json:"deliveries"`
	Opens       int     `json:"opens"`
	OpenRate    float64 `json:"open_rate"`
	ClickRate   float64 `json:"click_rate"`
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

// sumIncluded totals click counts, skipping URLs on excluded domains so the
// number matches the links the dashboard actually shows for an issue.
func (s *server) sumIncluded(counts map[string]int) int {
	n := 0
	for u, c := range counts {
		if !s.isExcluded(u) {
			n += c
		}
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

// rawClicksKey holds the all-time URL -> click count map shared by the stats,
// domains and issues responses.
const rawClicksKey = "_raw_clicks"

// cachedAllClicks returns all-time click counts. It checks the in-memory cache,
// then the disk cache, then falls back to the Buttondown API.
func (s *server) cachedAllClicks() (map[string]int, error) {
	const key = rawClicksKey
	if v, ok := s.cache.get(key); ok {
		return v.(map[string]int), nil
	}
	if s.disk != nil && s.disk.allClicksFresh() {
		counts := s.disk.getAllClicks()
		s.cache.set(key, counts)
		return counts, nil
	}
	d, err := fetchClicksSince(s.apiKey, time.Time{})
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
	s.cache.set(key, d.counts)
	if s.disk != nil {
		s.disk.setAllClicks(d.counts, d.total, d.newest)
	}
	return d.counts, nil
}

// Analytics cache tiers. Opens and clicks pile up in the first couple of days
// after an issue goes out and barely move afterwards, so how stale the cached
// numbers may be is tied to the issue's age rather than a single global TTL.
const (
	engagementSettleAge = 72 * time.Hour      // still accumulating engagement
	engagementFreshTTL  = 15 * time.Minute    // ...so recheck often
	engagementWarmAge   = 30 * 24 * time.Hour // mostly settled, still drifts
	engagementWarmTTL   = 6 * time.Hour
	engagementColdTTL   = 30 * 24 * time.Hour // done moving; cache hard
)

// publishDateFormats covers both shapes Buttondown returns for publish_date: a
// full RFC 3339 timestamp and a bare calendar date.
var publishDateFormats = []string{time.RFC3339, time.DateOnly}

func parsePublishDate(s string) (time.Time, error) {
	for _, f := range publishDateFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized publish date %q", s)
}

// engagementTTLFor returns how long cached analytics for an issue stay valid,
// based on how long ago it was published. An unparseable date is treated as
// recent on purpose: refetching too often is a cheap mistake, while assuming an
// unknown issue is old would silently freeze its numbers for a month.
func (s *server) engagementTTLFor(publishDate string) time.Duration {
	t, err := parsePublishDate(publishDate)
	if err != nil {
		return s.freshTTL()
	}
	switch age := time.Since(t); {
	case age < engagementSettleAge:
		return s.freshTTL()
	case age < engagementWarmAge:
		return engagementWarmTTL
	default:
		return engagementColdTTL
	}
}

// freshTTL caps the shortest analytics TTL at the configured refresh interval,
// so asking for faster refreshes isn't quietly overridden by a longer cache TTL.
func (s *server) freshTTL() time.Duration {
	if s.refreshInterval > 0 && s.refreshInterval < engagementFreshTTL {
		return s.refreshInterval
	}
	return engagementFreshTTL
}

func (s *server) cachedEmailAnalytics(emailID, publishDate string) (analytics, error) {
	memKey := "analytics:" + emailID
	if v, ok := s.cache.get(memKey); ok {
		return v.(analytics), nil
	}
	if s.disk != nil {
		if a, ok := s.disk.getAnalytics(emailID, s.engagementTTLFor(publishDate)); ok {
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
// in-memory cache, then disk cache, before calling the API. Like analytics,
// how long the cached breakdown is trusted depends on the issue's age.
func (s *server) cachedClicksForEmail(emailID, publishDate string) (map[string]int, error) {
	memKey := "email:" + emailID
	if v, ok := s.cache.get(memKey); ok {
		return v.(map[string]int), nil
	}
	if s.disk != nil {
		if counts, ok := s.disk.getIssueClicks(emailID, s.engagementTTLFor(publishDate)); ok {
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

	total := sumCounts(counts)
	totalLinks := len(counts)
	avgClicks, avgLinks := 0, 0
	if issueCount > 0 {
		avgClicks = total / issueCount
		avgLinks = totalLinks / issueCount
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
			counts, err := s.cachedClicksForEmail(e.ID, e.PublishDate)
			if err != nil {
				results[i] = result{err: err}
				return
			}
			sum := issueSummary{
				Number:      issueNumberFromSubject(e.Subject),
				EmailID:     e.ID,
				Subject:     e.Subject,
				Date:        e.PublishDate,
				TotalClicks: s.sumIncluded(counts),
			}
			// Analytics are best effort: a failure still leaves the click
			// counts worth serving, so log it and leave the rates zeroed.
			if a, aErr := s.cachedEmailAnalytics(e.ID, e.PublishDate); aErr != nil {
				fmt.Fprintf(os.Stderr, "issues: analytics for %s: %v\n", e.ID, aErr)
			} else {
				sum.Deliveries = a.Deliveries
				sum.Opens = a.Opens
				sum.OpenRate = a.OpenRate
				sum.ClickRate = a.ClickRate
			}
			results[i] = result{summary: sum}
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

// refreshAll refreshes raw click data from Buttondown, ignoring TTLs, and
// writes it to the in-memory and disk caches before rebuilding the derived
// responses. Seeding the raw counts first means loadStats/loadDomains reuse
// them instead of paginating the API again. On a fetch error it logs and leaves
// the existing caches intact, so handlers keep serving the last good data until
// the next tick.
// mergeCounts combines two click count maps into a new one. It never mutates
// its inputs: handlers may still be reading the cached map when a refresh runs.
func mergeCounts(base, delta map[string]int) map[string]int {
	merged := make(map[string]int, len(base)+len(delta))
	for u, c := range base {
		merged[u] = c
	}
	for u, c := range delta {
		merged[u] += c
	}
	return merged
}

// refreshAllClicks brings the cached click history up to date by fetching only
// the events that arrived since the last sync. Buttondown returns events
// newest-first, so the walk stops as soon as it reaches one already counted,
// and a page covers days of activity: a routine refresh is a single request no
// matter how large the history behind it grows.
//
// The event totals are the safety net. Cached events plus new events must equal
// the total the API reports; when it doesn't, something changed behind the mark
// (a deleted subscriber, an event arriving out of order) and the only honest
// response is to rebuild from scratch.
func (s *server) refreshAllClicks() {
	cached, mark, total, ok := s.clickState()
	if !ok {
		s.walkAllClicks()
		return
	}
	d, err := fetchClicksSince(s.apiKey, mark)
	if err != nil {
		fmt.Fprintf(os.Stderr, "refresh: clicks since %s: %v\n", mark.Format(time.RFC3339), err)
		return
	}
	if d.events == 0 && d.total == total {
		// Confirmed unchanged. The cache is current even though nothing was
		// downloaded, so reset its age instead of letting the TTL force a walk.
		s.disk.markAllClicksVerified()
		s.cache.set(rawClicksKey, cached)
		return
	}
	if total+d.events != d.total {
		fmt.Fprintf(os.Stderr, "refresh: click history drifted (%d cached + %d new != %d reported), rebuilding\n",
			total, d.events, d.total)
		s.walkAllClicks()
		return
	}
	merged := mergeCounts(cached, d.counts)
	s.cache.set(rawClicksKey, merged)
	s.disk.setAllClicks(merged, d.total, d.newest)
}

// clickState reports the cached click history to sync against, if there is one.
func (s *server) clickState() (map[string]int, time.Time, int, bool) {
	if s.disk == nil {
		return nil, time.Time{}, 0, false
	}
	return s.disk.allClicksState()
}

// walkAllClicks rebuilds the click history from scratch, replacing the cache.
func (s *server) walkAllClicks() {
	d, err := fetchClicksSince(s.apiKey, time.Time{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "refresh: all-clicks: %v\n", err)
		return
	}
	s.cache.set(rawClicksKey, d.counts)
	if s.disk != nil {
		s.disk.setAllClicks(d.counts, d.total, d.newest)
	}
}

func (s *server) refreshAll() {
	s.refreshAllClicks()

	var wg sync.WaitGroup
	wg.Add(3)
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
	go func() {
		defer wg.Done()
		if resp, err := s.loadIssues(); err == nil {
			s.cache.set("issues", resp)
		}
	}()
	wg.Wait()
}

// startRefreshLoop refreshes the cache once immediately, then on every interval
// tick for the lifetime of the process. A single goroutine owns the ticker, so
// refreshes never overlap even if one runs long.
func (s *server) startRefreshLoop(interval time.Duration) {
	s.refreshInterval = interval
	// Holding a response in memory for longer than the gap between refreshes
	// would make ticks no-ops, so no cache layer outlives the interval.
	s.cache.setTTL(min(defaultMemTTL, interval))
	go func() {
		s.refreshAll()
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			s.refreshAll()
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
			a, err := s.cachedEmailAnalytics(e.ID, e.PublishDate)
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
	e, err := lookupEmailByIssue(s.apiKey, n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	counts, err := s.cachedClicksForEmail(e.ID, e.PublishDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	filtered := make(map[string]int, len(counts))
	for u, c := range counts {
		if !s.isExcluded(u) {
			filtered[u] = c
		}
	}
	resp := issueStatsResponse{
		Issue:       n,
		Subject:     e.Subject,
		EmailID:     e.ID,
		TotalClicks: sumCounts(filtered),
		Links:       sortedLinks(filtered, 0),
	}
	s.cache.set(cacheKey, resp)
	writeJSON(w, resp)
}
