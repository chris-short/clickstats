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
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: system-ui, -apple-system, sans-serif; font-size: 13px;
           color: #1a1a2e; background: #fff; }

    /* ---- Header ---- */
    .report-header {
      display: flex; align-items: center; gap: 14px;
      padding: 24px 32px 20px;
      border-bottom: 3px solid #4f46e5;
    }
    .report-header img { width: 48px; height: 48px; flex-shrink: 0; }
    .report-header-text h1 { font-size: 20px; font-weight: 700; color: #1a1a2e; }
    .report-header-text .subtitle {
      font-size: 12px; color: #6b7280; margin-top: 2px; letter-spacing: .04em;
      text-transform: uppercase;
    }

    /* ---- Meta bar ---- */
    .meta-bar {
      display: flex; gap: 24px;
      padding: 12px 32px;
      background: #f8f9ff;
      border-bottom: 1px solid #e5e7eb;
      font-size: 12px; color: #6b7280;
    }
    .meta-bar strong { color: #1a1a2e; }

    /* ---- Content area ---- */
    .content { padding: 24px 32px; }

    /* ---- Sponsor block ---- */
    .sponsor-block {
      background: #f0fdf4; border: 1px solid #bbf7d0;
      border-radius: 8px; padding: 18px 20px; margin-bottom: 28px;
      page-break-inside: avoid;
    }
    .sponsor-block h2 {
      font-size: 11px; font-weight: 700; color: #166534;
      text-transform: uppercase; letter-spacing: .06em; margin-bottom: 14px;
    }
    .sponsor-grid { display: grid; grid-template-columns: 120px 1fr; gap: 6px 16px; }
    .sponsor-grid .label { color: #6b7280; font-size: 12px; }
    .sponsor-grid .value { font-size: 12px; font-weight: 600; word-break: break-all; }
    .sponsor-grid .value.url { color: #4f46e5; font-weight: 400; }

    /* ---- Links table ---- */
    .links-heading {
      font-size: 11px; font-weight: 700; color: #6b7280;
      text-transform: uppercase; letter-spacing: .06em;
      margin-bottom: 10px;
    }
    table.links { border-collapse: collapse; width: 100%; font-size: 12px; }
    table.links th {
      text-align: left; padding: 7px 10px;
      font-size: 10px; font-weight: 700;
      text-transform: uppercase; letter-spacing: .06em; color: #9ca3af;
      border-bottom: 2px solid #e5e7eb;
    }
    table.links th:last-child { text-align: right; }
    table.links td { padding: 6px 10px; border-bottom: 1px solid #f3f4f6; }
    table.links td:first-child { color: #9ca3af; font-size: 11px; width: 36px; }
    table.links td:last-child { text-align: right; font-weight: 600; width: 60px; }
    table.links td.url-cell { word-break: break-all; color: #374151; }
    table.links tbody tr:nth-child(even) td { background: #fafafa; }
    tr.sponsor-row td { background: #fefce8 !important; }
    tr.sponsor-row td.url-cell { color: #854d0e; font-weight: 600; }

    /* ---- Footer ---- */
    .report-footer {
      margin-top: 32px; padding: 14px 32px;
      border-top: 1px solid #e5e7eb;
      font-size: 11px; color: #9ca3af;
      display: flex; justify-content: space-between;
    }

    /* ---- Screen-only controls ---- */
    .no-print {
      position: fixed; bottom: 24px; right: 24px;
    }
    .no-print button {
      background: #4f46e5; color: #fff; border: none;
      padding: 10px 20px; border-radius: 6px; font-size: 14px;
      font-weight: 600; cursor: pointer; box-shadow: 0 2px 8px rgba(0,0,0,.2);
    }
    .no-print button:hover { background: #4338ca; }

    @media print {
      .no-print { display: none; }
      .report-header { padding: 16px 24px 14px; }
      .content { padding: 16px 24px; }
      .report-footer { padding: 10px 24px; }
      table.links { font-size: 11px; }
    }
  </style>
</head>
<body>
  <header class="report-header">
    <img src="/favicon.svg" alt="">
    <div class="report-header-text">
      <h1>{{.Name}} &mdash; Issue #{{.Issue}}</h1>
      <div class="subtitle">Click Report</div>
    </div>
  </header>

  <div class="meta-bar">
    <span><strong>Subject:</strong> {{.Subject}}</span>
    <span><strong>Generated:</strong> {{.Date}}</span>
    {{if .Sponsor}}<span><strong>Sponsor:</strong> {{.Sponsor.URL}}</span>{{end}}
  </div>

  <div class="content">
    {{if .Sponsor}}
    <div class="sponsor-block">
      <h2>Sponsor Performance</h2>
      <div class="sponsor-grid">
        <span class="label">Link</span>       <span class="value url">{{.Sponsor.URL}}</span>
        <span class="label">Clicks</span>     <span class="value">{{.Sponsor.Clicks}}</span>
        <span class="label">Rank</span>       <span class="value">#{{.Sponsor.Rank}} of {{.Sponsor.TotalLinks}} links</span>
        <span class="label">Click Rate</span> <span class="value">{{.Sponsor.ClickRate}}%</span>
      </div>
    </div>
    {{end}}

    <p class="links-heading">All Links &mdash; Ranked by Clicks</p>
    <table class="links">
      <thead><tr><th>#</th><th>URL</th><th>Clicks</th></tr></thead>
      <tbody>
        {{range $i, $l := .Links}}
        <tr{{if and $.Sponsor (eq $l.URL $.Sponsor.URL)}} class="sponsor-row"{{end}}>
          <td>{{inc $i}}</td>
          <td class="url-cell">{{$l.URL}}</td>
          <td>{{$l.Clicks}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>

  <footer class="report-footer">
    <span>{{.Name}} click analytics</span>
    <span>clickstats</span>
  </footer>

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
	counts, err := s.cachedClicksForEmail(emailID)
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
	links := sortedLinks(filtered, 0)

	var sp *sponsorData
	if sponsorURL := strings.TrimSpace(r.URL.Query().Get("sponsor")); sponsorURL != "" {
		clicks := filtered[sponsorURL]
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
