package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// scrapeRequest mirrors the Purify API request model.
type scrapeRequest struct {
	URL          string `json:"url"`
	OutputFormat string `json:"output_format,omitempty"`
	ExtractMode  string `json:"extract_mode,omitempty"`
}

// scrapeResponse mirrors the Purify API response model.
type scrapeResponse struct {
	Success  bool   `json:"success"`
	Content  string `json:"content"`
	Metadata *struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		SiteName    string `json:"site_name"`
		Author      string `json:"author"`
		Language    string `json:"language"`
		SourceURL   string `json:"source_url"`
	} `json:"metadata"`
	Tokens *struct {
		OriginalEstimate int     `json:"original_estimate"`
		CleanedEstimate  int     `json:"cleaned_estimate"`
		SavingsPercent   float64 `json:"savings_percent"`
	} `json:"tokens"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// batchResponse mirrors the Purify batch API response.
type batchResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Total  int    `json:"total"`
}

// batchStatusResponse mirrors the Purify batch status API response.
type batchStatusResponse struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	Completed int               `json:"completed"`
	Total     int               `json:"total"`
	Results   []json.RawMessage `json:"results"`
}

// crawlResponse mirrors the Purify crawl API response.
type crawlResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// crawlStatusResponse mirrors the Purify crawl status API response.
type crawlStatusResponse struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	Completed int               `json:"completed"`
	Total     int               `json:"total"`
	Results   []json.RawMessage `json:"results"`
}

// mapResponse mirrors the Purify map API response.
type mapResponse struct {
	Success bool     `json:"success"`
	URLs    []string `json:"urls"`
	Total   int      `json:"total"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// extractResponse mirrors the Purify extract API response.
type extractResponse struct {
	Success  bool            `json:"success"`
	Data     json.RawMessage `json:"data"`
	Metadata *struct {
		Title     string `json:"title"`
		SourceURL string `json:"source_url"`
	} `json:"metadata"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func main() {
	apiURL := os.Getenv("PURIFY_API_URL")
	if apiURL == "" {
		apiURL = "http://127.0.0.1:8080"
	}
	apiKey := os.Getenv("PURIFY_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "PURIFY_API_KEY is required")
		os.Exit(1)
	}

	s := server.NewMCPServer(
		"purify",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	scrapeURLTool := mcp.NewTool("scrape_url",
		mcp.WithDescription("Scrape a web page and return cleaned content (markdown/text/html). Uses a headless browser to render JavaScript-heavy pages."),
		mcp.WithString("url",
			mcp.Required(),
			mcp.Description("The URL of the web page to scrape"),
		),
		mcp.WithString("extract_mode",
			mcp.Description("Content extraction mode: 'readability' (default, extracts main article), 'raw' (full page HTML), 'pruning' (ML-based pruning), or 'auto' (automatic selection)"),
			mcp.Enum("readability", "raw", "pruning", "auto"),
		),
		mcp.WithString("output_format",
			mcp.Description("Output format: 'markdown' (default), 'text' (plain text), 'html', or 'markdown_citations'"),
			mcp.Enum("markdown", "text", "html", "markdown_citations"),
		),
	)

	s.AddTool(scrapeURLTool, handleScrapeURL(apiURL, apiKey))

	// batch_scrape tool
	batchScrapeTool := mcp.NewTool("batch_scrape",
		mcp.WithDescription("Scrape multiple URLs in parallel and return cleaned content for each. Useful for gathering content from many pages at once."),
		mcp.WithArray("urls",
			mcp.Required(),
			mcp.Description("List of URLs to scrape"),
		),
		mcp.WithString("output_format",
			mcp.Description("Output format: 'markdown' (default), 'text', 'html', or 'markdown_citations'"),
			mcp.Enum("markdown", "text", "html", "markdown_citations"),
		),
		mcp.WithString("extract_mode",
			mcp.Description("Content extraction mode: 'readability' (default), 'raw', 'pruning', or 'auto'"),
			mcp.Enum("readability", "raw", "pruning", "auto"),
		),
	)
	s.AddTool(batchScrapeTool, handleBatchScrape(apiURL, apiKey))

	// crawl_site tool
	crawlSiteTool := mcp.NewTool("crawl_site",
		mcp.WithDescription("Recursively crawl a website starting from a URL, following links up to a specified depth. Returns cleaned content for each discovered page."),
		mcp.WithString("url",
			mcp.Required(),
			mcp.Description("The starting URL to crawl from"),
		),
		mcp.WithNumber("max_depth",
			mcp.Description("Maximum crawl depth from the starting URL (default: 3, max: 10)"),
		),
		mcp.WithNumber("max_pages",
			mcp.Description("Maximum number of pages to crawl (default: 100, max: 500)"),
		),
		mcp.WithString("scope",
			mcp.Description("Link following scope: 'subdomain' (default), 'domain' (exact domain only), or 'page' (single page)"),
			mcp.Enum("subdomain", "domain", "page"),
		),
	)
	s.AddTool(crawlSiteTool, handleCrawlSite(apiURL, apiKey))

	// map_site tool
	mapSiteTool := mcp.NewTool("map_site",
		mcp.WithDescription("Discover all URLs on a website by crawling and extracting links. Returns a list of URLs without scraping their content."),
		mcp.WithString("url",
			mcp.Required(),
			mcp.Description("The URL of the website to map"),
		),
	)
	s.AddTool(mapSiteTool, handleMapSite(apiURL, apiKey))

	// extract_data tool
	extractDataTool := mcp.NewTool("extract_data",
		mcp.WithDescription("Scrape a web page and extract structured data using an LLM. Requires a JSON schema describing the desired output and an LLM API key."),
		mcp.WithString("url",
			mcp.Required(),
			mcp.Description("The URL of the web page to scrape"),
		),
		mcp.WithString("schema",
			mcp.Required(),
			mcp.Description("JSON schema string describing the desired output structure"),
		),
		mcp.WithString("llm_api_key",
			mcp.Required(),
			mcp.Description("API key for the LLM service (OpenAI-compatible)"),
		),
		mcp.WithString("llm_model",
			mcp.Description("LLM model to use (default: 'gpt-4o-mini')"),
		),
		mcp.WithString("llm_base_url",
			mcp.Description("Base URL for the LLM API (default: 'https://api.openai.com/v1'). Supports any OpenAI-compatible API."),
		),
	)
	s.AddTool(extractDataTool, handleExtractData(apiURL, apiKey))

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

// apiPost sends a POST request to the Purify API and returns the response body.
func apiPost(ctx context.Context, client *http.Client, apiURL, apiKey, path string, payload interface{}) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// pollJobCompletion polls a job endpoint until status is no longer "processing" or context is cancelled.
func pollJobCompletion(ctx context.Context, client *http.Client, apiURL, apiKey, endpoint string) ([]byte, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+endpoint, nil)
			if err != nil {
				return nil, fmt.Errorf("create poll request: %w", err)
			}
			req.Header.Set("X-API-Key", apiKey)

			resp, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("poll request failed: %w", err)
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return nil, fmt.Errorf("read poll response: %w", err)
			}

			// Quick check if still processing.
			var status struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(body, &status); err != nil {
				return nil, fmt.Errorf("parse poll status: %w", err)
			}

			if status.Status != "processing" {
				return body, nil
			}
		}
	}
}

func handleScrapeURL(apiURL, apiKey string) server.ToolHandlerFunc {
	client := &http.Client{Timeout: 120 * time.Second}

	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url, err := request.RequireString("url")
		if err != nil {
			return mcp.NewToolResultError("url is required"), nil
		}

		extractMode := request.GetString("extract_mode", "")
		outputFormat := request.GetString("output_format", "")

		reqBody := scrapeRequest{
			URL:          url,
			ExtractMode:  extractMode,
			OutputFormat: outputFormat,
		}

		body, err := json.Marshal(reqBody)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal request: %v", err)), nil
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/v1/scrape", bytes.NewReader(body))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create request: %v", err)), nil
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-API-Key", apiKey)

		resp, err := client.Do(httpReq)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("API request failed: %v", err)), nil
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to read response: %v", err)), nil
		}

		var scrapeResp scrapeResponse
		if err := json.Unmarshal(respBody, &scrapeResp); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to parse response: %v", err)), nil
		}

		if !scrapeResp.Success {
			errMsg := "scrape failed"
			if scrapeResp.Error != nil {
				errMsg = fmt.Sprintf("[%s] %s", scrapeResp.Error.Code, scrapeResp.Error.Message)
			}
			return mcp.NewToolResultError(errMsg), nil
		}

		// Build result with metadata header
		var result string
		if scrapeResp.Metadata != nil {
			m := scrapeResp.Metadata
			result = fmt.Sprintf("Title: %s\nSource: %s\n\n", m.Title, m.SourceURL)
		}
		result += scrapeResp.Content

		if scrapeResp.Tokens != nil {
			t := scrapeResp.Tokens
			result += fmt.Sprintf("\n\n---\nTokens: %d (saved %.0f%% from original %d)",
				t.CleanedEstimate, t.SavingsPercent, t.OriginalEstimate)
		}

		return mcp.NewToolResultText(result), nil
	}
}

func handleBatchScrape(apiURL, apiKey string) server.ToolHandlerFunc {
	client := &http.Client{Timeout: 600 * time.Second}

	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		urls, err := request.RequireStringSlice("urls")
		if err != nil {
			return mcp.NewToolResultError("urls is required and must be an array of strings"), nil
		}

		outputFormat := request.GetString("output_format", "")
		extractMode := request.GetString("extract_mode", "")

		payload := map[string]interface{}{
			"urls": urls,
			"options": map[string]interface{}{
				"output_format": outputFormat,
				"extract_mode":  extractMode,
			},
		}

		// POST to create batch job.
		respBody, err := apiPost(ctx, client, apiURL, apiKey, "/api/v1/batch/scrape", payload)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("batch request failed: %v", err)), nil
		}

		var batchResp batchResponse
		if err := json.Unmarshal(respBody, &batchResp); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to parse batch response: %v", err)), nil
		}

		if batchResp.ID == "" {
			return mcp.NewToolResultError("batch job creation failed"), nil
		}

		// Poll for completion.
		resultBody, err := pollJobCompletion(ctx, client, apiURL, apiKey, "/api/v1/batch/"+batchResp.ID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("polling batch job failed: %v", err)), nil
		}

		var statusResp batchStatusResponse
		if err := json.Unmarshal(resultBody, &statusResp); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to parse batch status: %v", err)), nil
		}

		// Format results.
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Batch %s: %s (%d/%d completed)\n\n", statusResp.ID, statusResp.Status, statusResp.Completed, statusResp.Total))

		for i, raw := range statusResp.Results {
			var sr scrapeResponse
			if err := json.Unmarshal(raw, &sr); err != nil {
				sb.WriteString(fmt.Sprintf("--- Result %d: parse error ---\n\n", i+1))
				continue
			}
			if sr.Success {
				title := ""
				if sr.Metadata != nil {
					title = sr.Metadata.Title
				}
				sb.WriteString(fmt.Sprintf("--- [%d] %s ---\n%s\n\n", i+1, title, sr.Content))
			} else {
				errMsg := "unknown error"
				if sr.Error != nil {
					errMsg = sr.Error.Message
				}
				sb.WriteString(fmt.Sprintf("--- [%d] FAILED: %s ---\n\n", i+1, errMsg))
			}
		}

		return mcp.NewToolResultText(sb.String()), nil
	}
}

func handleCrawlSite(apiURL, apiKey string) server.ToolHandlerFunc {
	client := &http.Client{Timeout: 600 * time.Second}

	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url, err := request.RequireString("url")
		if err != nil {
			return mcp.NewToolResultError("url is required"), nil
		}

		payload := map[string]interface{}{
			"url": url,
		}

		args := request.GetArguments()
		if maxDepth, ok := args["max_depth"]; ok {
			payload["max_depth"] = maxDepth
		}
		if maxPages, ok := args["max_pages"]; ok {
			payload["max_pages"] = maxPages
		}
		if scope := request.GetString("scope", ""); scope != "" {
			payload["scope"] = scope
		}

		// POST to create crawl job.
		respBody, err := apiPost(ctx, client, apiURL, apiKey, "/api/v1/crawl", payload)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("crawl request failed: %v", err)), nil
		}

		var crawlResp crawlResponse
		if err := json.Unmarshal(respBody, &crawlResp); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to parse crawl response: %v", err)), nil
		}

		if crawlResp.ID == "" {
			return mcp.NewToolResultError("crawl job creation failed"), nil
		}

		// Poll for completion.
		resultBody, err := pollJobCompletion(ctx, client, apiURL, apiKey, "/api/v1/crawl/"+crawlResp.ID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("polling crawl job failed: %v", err)), nil
		}

		var statusResp crawlStatusResponse
		if err := json.Unmarshal(resultBody, &statusResp); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to parse crawl status: %v", err)), nil
		}

		// Format results.
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Crawl %s: %s (%d/%d pages)\n\n", statusResp.ID, statusResp.Status, statusResp.Completed, statusResp.Total))

		for i, raw := range statusResp.Results {
			var sr scrapeResponse
			if err := json.Unmarshal(raw, &sr); err != nil {
				sb.WriteString(fmt.Sprintf("--- Page %d: parse error ---\n\n", i+1))
				continue
			}
			if sr.Success {
				title := ""
				source := ""
				if sr.Metadata != nil {
					title = sr.Metadata.Title
					source = sr.Metadata.SourceURL
				}
				sb.WriteString(fmt.Sprintf("--- Page %d: %s (%s) ---\n%s\n\n", i+1, title, source, sr.Content))
			} else {
				errMsg := "unknown error"
				if sr.Error != nil {
					errMsg = sr.Error.Message
				}
				sb.WriteString(fmt.Sprintf("--- Page %d: FAILED: %s ---\n\n", i+1, errMsg))
			}
		}

		return mcp.NewToolResultText(sb.String()), nil
	}
}

func handleMapSite(apiURL, apiKey string) server.ToolHandlerFunc {
	client := &http.Client{Timeout: 120 * time.Second}

	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url, err := request.RequireString("url")
		if err != nil {
			return mcp.NewToolResultError("url is required"), nil
		}

		respBody, err := apiPost(ctx, client, apiURL, apiKey, "/api/v1/map", map[string]string{"url": url})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("map request failed: %v", err)), nil
		}

		var mapResp mapResponse
		if err := json.Unmarshal(respBody, &mapResp); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to parse map response: %v", err)), nil
		}

		if !mapResp.Success {
			errMsg := "map failed"
			if mapResp.Error != nil {
				errMsg = fmt.Sprintf("[%s] %s", mapResp.Error.Code, mapResp.Error.Message)
			}
			return mcp.NewToolResultError(errMsg), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Found %d URLs:\n\n", mapResp.Total))
		for _, u := range mapResp.URLs {
			sb.WriteString(u + "\n")
		}

		return mcp.NewToolResultText(sb.String()), nil
	}
}

func handleExtractData(apiURL, apiKey string) server.ToolHandlerFunc {
	client := &http.Client{Timeout: 120 * time.Second}

	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url, err := request.RequireString("url")
		if err != nil {
			return mcp.NewToolResultError("url is required"), nil
		}

		schemaStr, err := request.RequireString("schema")
		if err != nil {
			return mcp.NewToolResultError("schema is required"), nil
		}

		llmAPIKey, err := request.RequireString("llm_api_key")
		if err != nil {
			return mcp.NewToolResultError("llm_api_key is required"), nil
		}

		// Validate schema is valid JSON.
		var schemaJSON json.RawMessage
		if err := json.Unmarshal([]byte(schemaStr), &schemaJSON); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("schema must be valid JSON: %v", err)), nil
		}

		payload := map[string]interface{}{
			"url":         url,
			"schema":      schemaJSON,
			"llm_api_key": llmAPIKey,
		}

		if llmModel := request.GetString("llm_model", ""); llmModel != "" {
			payload["llm_model"] = llmModel
		}
		if llmBaseURL := request.GetString("llm_base_url", ""); llmBaseURL != "" {
			payload["llm_base_url"] = llmBaseURL
		}

		respBody, err := apiPost(ctx, client, apiURL, apiKey, "/api/v1/extract", payload)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("extract request failed: %v", err)), nil
		}

		var extResp extractResponse
		if err := json.Unmarshal(respBody, &extResp); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to parse extract response: %v", err)), nil
		}

		if !extResp.Success {
			errMsg := "extraction failed"
			if extResp.Error != nil {
				errMsg = fmt.Sprintf("[%s] %s", extResp.Error.Code, extResp.Error.Message)
			}
			return mcp.NewToolResultError(errMsg), nil
		}

		// Format the extracted data as pretty JSON.
		var prettyData bytes.Buffer
		if err := json.Indent(&prettyData, extResp.Data, "", "  "); err != nil {
			// Fall back to raw JSON.
			prettyData.Write(extResp.Data)
		}

		var result string
		if extResp.Metadata != nil {
			result = fmt.Sprintf("Source: %s\nTitle: %s\n\n", extResp.Metadata.SourceURL, extResp.Metadata.Title)
		}
		result += "Extracted Data:\n" + prettyData.String()

		return mcp.NewToolResultText(result), nil
	}
}
