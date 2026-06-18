package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type sponsorData struct {
	URL        string
	Clicks     int
	Rank       int
	TotalLinks int
	ClickRate  string
}

type printData struct {
	Issue   int
	Name    string
	Subject string
	Date    string
	Links   []linkCount
	Sponsor *sponsorData
}

var printTmpl = template.Must(template.New("print").Funcs(template.FuncMap{
	"inc": func(i int) int { return i + 1 },
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>{{.Name}} Issue #{{.Issue}} Click Report</title>
  <style>
    body { font-family: Georgia, serif; max-width: 720px; margin: 40px auto;
           color: #1a1a2e; padding: 0 20px; }
    h1 { font-size: 22px; margin-bottom: 4px; }
    .meta { color: #6b7280; font-size: 13px; margin-bottom: 32px; }
    .sponsor-block { background: #f0fdf4; border: 1px solid #bbf7d0;
                     border-radius: 6px; padding: 20px; margin-bottom: 32px; }
    .sponsor-block h2 { font-size: 15px; color: #166534; margin-bottom: 12px; }
    .sponsor-block table { border-collapse: collapse; width: 100%; }
    .sponsor-block td { padding: 5px 0; font-size: 14px; }
    .sponsor-block td:first-child { color: #6b7280; width: 140px; }
    h2.links-heading { font-size: 15px; margin-bottom: 12px; }
    table.links { border-collapse: collapse; width: 100%; font-size: 13px; }
    table.links th { text-align: left; padding: 6px 8px; font-size: 11px;
                     text-transform: uppercase; color: #6b7280;
                     border-bottom: 2px solid #e5e7eb; }
    table.links td { padding: 6px 8px; border-bottom: 1px solid #f3f4f6;
                     word-break: break-all; }
    tr.sponsor-row td { background: #fefce8; font-weight: 600; }
    .no-print { margin-top: 24px; }
    @media print { .no-print { display: none; } }
  </style>
</head>
<body>
  <h1>{{.Name}} Issue #{{.Issue}}</h1>
  <p class="meta">Click Report | {{.Date}}</p>

  {{if .Sponsor}}
  <div class="sponsor-block">
    <h2>Sponsor Performance</h2>
    <table>
      <tr><td>Link</td><td>{{.Sponsor.URL}}</td></tr>
      <tr><td>Clicks</td><td>{{.Sponsor.Clicks}}</td></tr>
      <tr><td>Rank</td><td>#{{.Sponsor.Rank}} of {{.Sponsor.TotalLinks}} links</td></tr>
      <tr><td>Click Rate</td><td>{{.Sponsor.ClickRate}}%</td></tr>
    </table>
  </div>
  {{end}}

  <h2 class="links-heading">All Links - Ranked by Clicks</h2>
  <table class="links">
    <thead><tr><th>#</th><th>URL</th><th>Clicks</th></tr></thead>
    <tbody>
      {{range $i, $l := .Links}}
      <tr{{if and $.Sponsor (eq $l.URL $.Sponsor.URL)}} class="sponsor-row"{{end}}>
        <td>{{inc $i}}</td><td>{{$l.URL}}</td><td>{{$l.Clicks}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>

  <div class="no-print">
    <button onclick="window.print()">Print / Save as PDF</button>
  </div>
</body>
</html>
`))

func (s *server) handlePrint(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		http.Error(w, "invalid issue number", http.StatusBadRequest)
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
	links := sortedLinks(counts, 0)

	var sp *sponsorData
	if sponsorURL := strings.TrimSpace(r.URL.Query().Get("sponsor")); sponsorURL != "" {
		clicks := counts[sponsorURL]
		rank := 1
		for _, l := range links {
			if l.Clicks > clicks {
				rank++
			}
		}
		rate := 0.0
		if recipients, err := fetchDeliveryCount(s.apiKey, emailID); err == nil && recipients > 0 {
			rate = float64(clicks) / float64(recipients) * 100
		}
		sp = &sponsorData{
			URL:        sponsorURL,
			Clicks:     clicks,
			Rank:       rank,
			TotalLinks: len(links),
			ClickRate:  fmt.Sprintf("%.2f", rate),
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := printTmpl.Execute(w, printData{
		Issue:   n,
		Name:    s.name,
		Subject: subject,
		Date:    time.Now().Format("January 2, 2006"),
		Links:   links,
		Sponsor: sp,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "printTmpl.Execute: %v\n", err)
	}
}
