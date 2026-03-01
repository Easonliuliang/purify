package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// CLI flags
var (
	apiURL  = flag.String("api-url", "http://localhost:8080", "Purify API base URL")
	apiKey  = flag.String("api-key", "", "API key for authenticated requests")
	runs    = flag.Int("runs", 3, "Number of runs per URL for averaging")
	output  = flag.String("output", "benchmark-results.json", "JSON output file path")
)

// Test URLs covering 5 site types.
var testURLs = []struct {
	Label string
	URL   string
}{
	{"Static", "https://example.com"},
	{"Blog", "https://go.dev/blog/go1.21"},
	{"Docs", "https://go.dev/doc/effective_go"},
	{"News", "https://www.bbc.com/news"},
	{"Complex", "https://github.com/go-rod/rod"},
}

// --- Request / Response types (mirrors models package) ---

type scrapeRequest struct {
	URL          string `json:"url"`
	OutputFormat string `json:"output_format"`
	Timeout      int    `json:"timeout"`
}

type scrapeResponse struct {
	Success    bool       `json:"success"`
	StatusCode int        `json:"status_code"`
	Content    string     `json:"content"`
	Metadata   metadata   `json:"metadata"`
	Links      links      `json:"links"`
	Tokens     tokenInfo  `json:"tokens"`
	Timing     timingInfo `json:"timing"`
	Error      *errorDetail `json:"error,omitempty"`
}

type metadata struct {
	Title string `json:"title"`
}

type links struct {
	Internal []link `json:"internal"`
	External []link `json:"external"`
}

type link struct {
	Href string `json:"href"`
}

type tokenInfo struct {
	OriginalEstimate int     `json:"original_estimate"`
	CleanedEstimate  int     `json:"cleaned_estimate"`
	SavingsPercent   float64 `json:"savings_percent"`
}

type timingInfo struct {
	TotalMs      int64 `json:"total_ms"`
	NavigationMs int64 `json:"navigation_ms"`
	CleaningMs   int64 `json:"cleaning_ms"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// --- Benchmark result types ---

type runResult struct {
	Run            int     `json:"run"`
	TotalMs        int64   `json:"total_ms"`
	NavigationMs   int64   `json:"navigation_ms"`
	CleaningMs     int64   `json:"cleaning_ms"`
	OriginalTokens int     `json:"original_tokens"`
	CleanedTokens  int     `json:"cleaned_tokens"`
	SavingsPercent float64 `json:"savings_percent"`
	ContentLength  int     `json:"content_length"`
	StatusCode     int     `json:"status_code"`
	HasTitle       bool    `json:"has_title"`
	HasLinks       bool    `json:"has_links"`
	Success        bool    `json:"success"`
	Error          string  `json:"error,omitempty"`
}

type urlAverages struct {
	TotalMs        float64 `json:"total_ms"`
	NavigationMs   float64 `json:"navigation_ms"`
	CleaningMs     float64 `json:"cleaning_ms"`
	SavingsPercent float64 `json:"savings_percent"`
	ContentLength  float64 `json:"content_length"`
}

type urlResult struct {
	URL      string      `json:"url"`
	Label    string      `json:"label"`
	Runs     []runResult `json:"runs"`
	Averages *urlAverages `json:"averages,omitempty"`
}

type benchmarkReport struct {
	Timestamp string      `json:"timestamp"`
	APIURL    string      `json:"api_url"`
	RunsPerURL int        `json:"runs_per_url"`
	Results   []urlResult `json:"results"`
}

func main() {
	flag.Parse()

	fmt.Println("=== Purify Benchmark Suite ===")
	fmt.Printf("API URL:   %s\n", *apiURL)
	fmt.Printf("Runs/URL:  %d\n", *runs)
	fmt.Printf("Output:    %s\n", *output)
	fmt.Println()

	// Quick connectivity check.
	if err := checkAPI(*apiURL); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot reach API at %s: %v\n", *apiURL, err)
		fmt.Fprintf(os.Stderr, "Make sure Purify is running (e.g. make run)\n")
		os.Exit(1)
	}

	report := benchmarkReport{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		APIURL:     *apiURL,
		RunsPerURL: *runs,
	}

	for _, t := range testURLs {
		fmt.Printf("Benchmarking [%s] %s ...\n", t.Label, t.URL)
		ur := urlResult{URL: t.URL, Label: t.Label}

		for i := 1; i <= *runs; i++ {
			fmt.Printf("  Run %d/%d ... ", i, *runs)
			rr := benchmarkURL(t.URL, i)
			if rr.Success {
				fmt.Printf("OK  %dms  %.1f%% saved\n", rr.TotalMs, rr.SavingsPercent)
			} else {
				fmt.Printf("FAILED: %s\n", rr.Error)
			}
			ur.Runs = append(ur.Runs, rr)
		}

		ur.Averages = computeAverages(ur.Runs)
		report.Results = append(report.Results, ur)
		fmt.Println()
	}

	// Print summary table.
	printTable(report.Results)

	// Write JSON report.
	if err := writeJSON(*output, report); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing JSON output: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nDetailed results written to %s\n", *output)
}

func checkAPI(baseURL string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(baseURL + "/api/v1/health")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func benchmarkURL(url string, run int) runResult {
	rr := runResult{Run: run}

	reqBody := scrapeRequest{
		URL:          url,
		OutputFormat: "markdown",
		Timeout:      60,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		rr.Error = fmt.Sprintf("marshal error: %v", err)
		return rr
	}

	req, err := http.NewRequest("POST", *apiURL+"/api/v1/scrape", bytes.NewReader(bodyBytes))
	if err != nil {
		rr.Error = fmt.Sprintf("request error: %v", err)
		return rr
	}
	req.Header.Set("Content-Type", "application/json")
	if *apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+*apiKey)
	}

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		rr.Error = fmt.Sprintf("request failed: %v", err)
		return rr
	}
	defer resp.Body.Close()

	var sr scrapeResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		rr.Error = fmt.Sprintf("decode error: %v", err)
		return rr
	}

	rr.Success = sr.Success
	rr.StatusCode = sr.StatusCode
	rr.TotalMs = sr.Timing.TotalMs
	rr.NavigationMs = sr.Timing.NavigationMs
	rr.CleaningMs = sr.Timing.CleaningMs
	rr.OriginalTokens = sr.Tokens.OriginalEstimate
	rr.CleanedTokens = sr.Tokens.CleanedEstimate
	rr.SavingsPercent = sr.Tokens.SavingsPercent
	rr.ContentLength = len(sr.Content)
	rr.HasTitle = sr.Metadata.Title != ""
	rr.HasLinks = len(sr.Links.Internal)+len(sr.Links.External) > 0

	if sr.Error != nil {
		rr.Error = sr.Error.Message
	}

	return rr
}

func computeAverages(runs []runResult) *urlAverages {
	var successCount int
	var avg urlAverages

	for _, r := range runs {
		if !r.Success {
			continue
		}
		successCount++
		avg.TotalMs += float64(r.TotalMs)
		avg.NavigationMs += float64(r.NavigationMs)
		avg.CleaningMs += float64(r.CleaningMs)
		avg.SavingsPercent += r.SavingsPercent
		avg.ContentLength += float64(r.ContentLength)
	}

	if successCount == 0 {
		return nil
	}

	n := float64(successCount)
	avg.TotalMs /= n
	avg.NavigationMs /= n
	avg.CleaningMs /= n
	avg.SavingsPercent /= n
	avg.ContentLength /= n
	return &avg
}

func printTable(results []urlResult) {
	fmt.Println(strings.Repeat("─", 85))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "URL\tAvg Latency\tTokens Saved\tContent Len\tStatus\n")
	fmt.Fprintf(w, "───\t───────────\t────────────\t───────────\t──────\n")

	for _, r := range results {
		if r.Averages == nil {
			fmt.Fprintf(w, "%s\tFAILED\t-\t-\t-\n", truncateURL(r.URL, 40))
			continue
		}

		// Determine dominant status code from runs.
		status := dominantStatus(r.Runs)

		fmt.Fprintf(w, "%s\t%dms\t%.1f%%\t%s\t%d\n",
			truncateURL(r.URL, 40),
			int64(r.Averages.TotalMs),
			r.Averages.SavingsPercent,
			formatInt(int(r.Averages.ContentLength)),
			status,
		)
	}

	w.Flush()
	fmt.Println(strings.Repeat("─", 85))
}

func dominantStatus(runs []runResult) int {
	counts := map[int]int{}
	for _, r := range runs {
		if r.Success {
			counts[r.StatusCode]++
		}
	}
	best, bestCount := 0, 0
	for code, count := range counts {
		if count > bestCount {
			best = code
			bestCount = count
		}
	}
	return best
}

func truncateURL(u string, max int) string {
	if len(u) <= max {
		return u
	}
	return u[:max-3] + "..."
}

func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

func writeJSON(path string, report benchmarkReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
