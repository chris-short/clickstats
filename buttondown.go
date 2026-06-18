package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
)

var buttondownBase = "https://api.buttondown.com/v1"

var metadataURLKeys = []string{"url", "link", "link_url", "clicked_url"}

type emailEvent struct {
	Metadata map[string]string `json:"metadata"`
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
	Recipients int     `json:"recipients"`
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

func fetchAllClicks(apiKey string) (map[string]int, error) {
	return fetchClicksFromURL(apiKey, buttondownBase+"/events?event_type=clicked")
}

func fetchClicksForEmail(apiKey, emailID string) (map[string]int, error) {
	return fetchClicksFromURL(apiKey, buttondownBase+"/events?event_type=clicked&email_id="+emailID)
}

func lookupEmailByIssue(apiKey string, issue int) (id, subject string, err error) {
	url := fmt.Sprintf("%s/emails?subject=%d&excluded_fields=body", buttondownBase, issue)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Token "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("API returned %s", resp.Status)
	}
	var p emailsPage
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return "", "", err
	}
	if len(p.Results) == 0 {
		return "", "", fmt.Errorf("no email found with subject containing %d", issue)
	}
	return p.Results[0].ID, p.Results[0].Subject, nil
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
	// Compute rates from counts when Buttondown doesn't return them directly.
	if a.OpenRate == 0 && a.Opens > 0 && a.Recipients > 0 {
		a.OpenRate = float64(a.Opens) / float64(a.Recipients) * 100
	}
	if a.ClickRate == 0 && a.Clicks > 0 && a.Recipients > 0 {
		a.ClickRate = float64(a.Clicks) / float64(a.Recipients) * 100
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

func fetchRecipientCount(apiKey, emailID string) (int, error) {
	a, err := fetchEmailAnalytics(apiKey, emailID)
	if err != nil {
		return 0, err
	}
	return a.Recipients, nil
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
