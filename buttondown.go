package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

var buttondownBase = "https://api.buttondown.com/v1"

var metadataURLKeys = []string{"url", "link", "link_url", "clicked_url"}

type emailEvent struct {
	CreationDate time.Time         `json:"creation_date"`
	Metadata     map[string]string `json:"metadata"`
}

type eventsPage struct {
	Results []emailEvent `json:"results"`
	Next    *string      `json:"next"`
	Count   int          `json:"count"`
}

type email struct {
	ID          string `json:"id"`
	Subject     string `json:"subject"`
	PublishDate string `json:"publish_date"`
}

type emailsPage struct {
	Results []email `json:"results"`
	Count   int     `json:"count"`
	Next    *string `json:"next"`
}

type analytics struct {
	Deliveries int     `json:"deliveries"`
	Opens      int     `json:"opens"`
	Clicks     int     `json:"clicks"`
	OpenRate   float64 `json:"open_rate"`
	ClickRate  float64 `json:"click_rate"`
}

func fetchPage(apiKey, url string) (*eventsPage, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned %s", resp.Status)
	}
	var p eventsPage
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func extractURL(meta map[string]string) string {
	for _, key := range metadataURLKeys {
		if v, ok := meta[key]; ok && v != "" {
			return v
		}
	}
	return ""
}

// fetchClicksFromURL paginates through all click events starting at startURL
// and returns a map of URL -> click count.
func fetchClicksFromURL(apiKey, startURL string) (map[string]int, error) {
	counts := map[string]int{}
	nextURL := startURL
	for nextURL != "" {
		p, err := fetchPage(apiKey, nextURL)
		if err != nil {
			return nil, err
		}
		for _, e := range p.Results {
			if u := extractURL(e.Metadata); u != "" {
				counts[u]++
			}
		}
		if p.Next == nil || len(p.Results) == 0 {
			break
		}
		nextURL = *p.Next
	}
	return counts, nil
}

// clickDelta is the result of a click history sync: the click counts found,
// how many events they came from, the newest event timestamp seen, and the
// collection total the API reported.
type clickDelta struct {
	counts map[string]int
	events int // includes events carrying no usable URL, so it can be reconciled against total
	newest time.Time
	total  int
}

// fetchClicksSince walks click events newest-first and stops at the first event
// at or before mark, so its cost tracks how many clicks arrived since the last
// sync rather than how many exist. A zero mark walks the whole history.
//
// Ordering is requested explicitly rather than relying on the collection's
// default, since the early stop is only correct if newer events come first.
func fetchClicksSince(apiKey string, mark time.Time) (clickDelta, error) {
	d := clickDelta{counts: map[string]int{}}
	nextURL := buttondownBase + "/events?event_type=clicked&ordering=-creation_date"
	for page := 0; nextURL != ""; page++ {
		p, err := fetchPage(apiKey, nextURL)
		if err != nil {
			return clickDelta{}, err
		}
		if page == 0 {
			d.total = p.Count
		}
		for _, e := range p.Results {
			// A zero mark means "take everything", so the stop is skipped
			// rather than compared: an event with no usable timestamp would
			// otherwise end a full walk on its first page.
			if !mark.IsZero() && !e.CreationDate.After(mark) {
				return d, nil // caught up with what the cache already counted
			}
			if e.CreationDate.After(d.newest) {
				d.newest = e.CreationDate
			}
			d.events++
			if u := extractURL(e.Metadata); u != "" {
				d.counts[u]++
			}
		}
		if p.Next == nil || len(p.Results) == 0 {
			break
		}
		nextURL = *p.Next
	}
	return d, nil
}

// fetchAllClicks walks the entire click history. The CLI uses it directly; the
// server syncs incrementally through fetchClicksSince.
func fetchAllClicks(apiKey string) (map[string]int, error) {
	d, err := fetchClicksSince(apiKey, time.Time{})
	return d.counts, err
}

func fetchClicksForEmail(apiKey, emailID string) (map[string]int, error) {
	return fetchClicksFromURL(apiKey, buttondownBase+"/events?event_type=clicked&email_id="+emailID)
}

// lookupEmailByIssue returns the whole email rather than just its ID, since
// callers need the publish date to decide how long its data may be cached.
func lookupEmailByIssue(apiKey string, issue int) (email, error) {
	url := fmt.Sprintf("%s/emails?subject=%d&excluded_fields=body", buttondownBase, issue)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return email{}, err
	}
	req.Header.Set("Authorization", "Token "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return email{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return email{}, fmt.Errorf("API returned %s", resp.Status)
	}
	var p emailsPage
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return email{}, err
	}
	if len(p.Results) == 0 {
		return email{}, fmt.Errorf("no email found with subject containing %d", issue)
	}
	return p.Results[0], nil
}

func fetchSentEmailsPage(apiKey string) (*emailsPage, error) {
	url := fmt.Sprintf("%s/emails?status=sent&excluded_fields=body&page=1&page_size=100&ordering=-creation_date", buttondownBase)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned %s", resp.Status)
	}
	var p emailsPage
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func fetchRecentEmails(apiKey string, n int) ([]email, error) {
	p, err := fetchSentEmailsPage(apiKey)
	if err != nil {
		return nil, err
	}
	if len(p.Results) > n {
		return p.Results[:n], nil
	}
	return p.Results, nil
}

func fetchEmailCount(apiKey string) (int, error) {
	p, err := fetchSentEmailsPage(apiKey)
	if err != nil {
		return 0, err
	}
	return p.Count, nil
}

func fetchEmailAnalytics(apiKey, emailID string) (analytics, error) {
	url := fmt.Sprintf("%s/emails/%s/analytics", buttondownBase, emailID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return analytics{}, err
	}
	req.Header.Set("Authorization", "Token "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return analytics{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return analytics{}, fmt.Errorf("API returned %s", resp.Status)
	}
	var a analytics
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		return analytics{}, err
	}
	// Buttondown returns deliveries (not recipients) as the denominator.
	// Compute rates from counts when the API doesn't return them directly.
	if a.OpenRate == 0 && a.Opens > 0 && a.Deliveries > 0 {
		a.OpenRate = float64(a.Opens) / float64(a.Deliveries) * 100
	}
	if a.ClickRate == 0 && a.Clicks > 0 && a.Deliveries > 0 {
		a.ClickRate = float64(a.Clicks) / float64(a.Deliveries) * 100
	}
	// If API returns rates as decimals (0.0-1.0), convert to percentages.
	if a.OpenRate > 0 && a.OpenRate <= 1 {
		a.OpenRate *= 100
	}
	if a.ClickRate > 0 && a.ClickRate <= 1 {
		a.ClickRate *= 100
	}
	return a, nil
}

func fetchDeliveryCount(apiKey, emailID string) (int, error) {
	a, err := fetchEmailAnalytics(apiKey, emailID)
	if err != nil {
		return 0, err
	}
	return a.Deliveries, nil
}

// fetchAllNewsletterEmails paginates through all sent emails in chronological order.
func fetchAllNewsletterEmails(apiKey string) ([]email, error) {
	var all []email
	nextURL := fmt.Sprintf("%s/emails?status=sent&excluded_fields=body&page_size=100&ordering=creation_date", buttondownBase)
	for nextURL != "" {
		req, err := http.NewRequest("GET", nextURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Token "+apiKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("API returned %s", resp.Status)
		}
		var p emailsPage
		err = json.NewDecoder(resp.Body).Decode(&p)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		all = append(all, p.Results...)
		if p.Next == nil || len(p.Results) == 0 {
			break
		}
		nextURL = *p.Next
	}
	return all, nil
}

var issueNumRe = regexp.MustCompile(`(\d+)(?:\s*:|$)`)

func issueNumberFromSubject(subject string) int {
	m := issueNumRe.FindStringSubmatch(subject)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}
