package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		runServe(os.Args[2:])
		return
	}

	emailID := flag.String("email-id", "", "filter clicks to a specific email UUID")
	issue := flag.Int("issue", 0, "filter clicks by issue number (looks up email ID automatically)")
	debug := flag.Bool("debug", false, "print raw metadata from first click event and exit")
	flag.Parse()

	apiKey := os.Getenv("BUTTONDOWN_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "BUTTONDOWN_API_KEY not set")
		os.Exit(1)
	}

	if *issue != 0 {
		id, subject, err := lookupEmailByIssue(apiKey, *issue)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error looking up issue %d: %v\n", *issue, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "matched: %s (%s)\n", subject, id)
		*emailID = id
	}

	if *debug {
		startURL := buttondownBase + "/events?event_type=clicked&page=1"
		if *emailID != "" {
			startURL += "&email_id=" + *emailID
		}
		p, err := fetchPage(apiKey, startURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		for _, e := range p.Results {
			if len(e.Metadata) > 0 {
				fmt.Println("First click event metadata (raw):")
				for k, v := range e.Metadata {
					fmt.Printf("  %q: %q\n", k, v)
				}
				return
			}
		}
		fmt.Println("No click events found with metadata.")
		return
	}

	var counts map[string]int
	var err error
	if *emailID != "" {
		counts, err = fetchClicksForEmail(apiKey, *emailID)
	} else {
		fmt.Fprintln(os.Stderr, "fetching all click events...")
		counts, err = fetchAllClicks(apiKey)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if len(counts) == 0 {
		fmt.Println("No click events found.")
		if *emailID == "" {
			fmt.Println("Make sure click tracking is enabled in your Buttondown settings.")
		}
		return
	}

	type entry struct {
		url   string
		count int
	}
	entries := make([]entry, 0, len(counts))
	for u, c := range counts {
		entries = append(entries, entry{u, c})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].url < entries[j].url
	})

	sep := strings.Repeat("-", 6)
	fmt.Printf("%-6s  %s\n", "CLICKS", "URL")
	fmt.Printf("%-6s  %s\n", sep, strings.Repeat("-", 60))
	for _, e := range entries {
		fmt.Printf("%-6d  %s\n", e.count, e.url)
	}
}
