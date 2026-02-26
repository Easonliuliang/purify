package cleaner

import (
	"math"

	readability "github.com/go-shiori/go-readability"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/use-agent/purify/models"
)

// Cleaner orchestrates the two-stage cleaning pipeline:
//
//	Stage 1 (readability): extract main content, strip nav/footer/sidebar/ads
//	Stage 2 (markdown):    convert clean HTML → Markdown (or html/text pass-through)
//
// The converter is created once and reused across all requests (goroutine-safe).
type Cleaner struct {
	mdConverter *converter.Converter
}

// NewCleaner initialises the Cleaner with a pre-configured Markdown converter.
func NewCleaner() *Cleaner {
	return &Cleaner{
		mdConverter: newMarkdownConverter(),
	}
}

// Clean runs the full pipeline and returns a partial ScrapeResponse
// (Content + Metadata + Tokens filled; Timing is left to the API layer).
//
// Flow:
//  1. Estimate original tokens from raw HTML.
//  2. Stage 1: go-readability extracts main content.
//     Fallback: if extraction fails or content is too short, use raw HTML.
//  3. Stage 2: convert to the requested output format.
//  4. Estimate cleaned tokens and compute savings.
//  5. Assemble and return the partial response.
func (c *Cleaner) Clean(rawHTML string, sourceURL string, format string, extractMode string) (*models.ScrapeResponse, error) {
	// ── 1. Original token estimate ──────────────────────────────────
	originalTokens := EstimateTokens(rawHTML)

	// ── 2. Stage 1: Content extraction ──────────────────────────────
	var article readability.Article
	if extractMode == "raw" {
		// Skip readability; use the full rendered HTML as-is.
		article = fallbackArticle(rawHTML)
	} else {
		article, _ = ExtractContent(rawHTML, sourceURL)
	}

	// ── 3. Stage 2: Format conversion ───────────────────────────────
	var content string
	var err error

	switch format {
	case "markdown", "":
		content, err = ToMarkdown(c.mdConverter, article.Content, sourceURL)
		if err != nil {
			return nil, models.NewScrapeError(
				models.ErrCodeReadability,
				"markdown conversion failed",
				err,
			)
		}
	case "html":
		// Return the readability-cleaned HTML as-is.
		content = article.Content
	case "text":
		// Return the plain text extracted by readability.
		content = article.TextContent
	default:
		// Defensive: treat unknown formats as markdown.
		content, err = ToMarkdown(c.mdConverter, article.Content, sourceURL)
		if err != nil {
			return nil, models.NewScrapeError(
				models.ErrCodeReadability,
				"markdown conversion failed",
				err,
			)
		}
	}

	// ── 4. Cleaned token estimate + savings ─────────────────────────
	cleanedTokens := EstimateTokens(content)

	savingsPercent := 0.0
	if originalTokens > 0 {
		savingsPercent = float64(originalTokens-cleanedTokens) / float64(originalTokens) * 100
		// Round to 2 decimal places.
		savingsPercent = math.Round(savingsPercent*100) / 100
	}

	// ── 5. Assemble partial response ────────────────────────────────
	return &models.ScrapeResponse{
		Success: true,
		Content: content,
		Metadata: models.Metadata{
			Title:       article.Title,
			Description: article.Excerpt,
			SiteName:    article.SiteName,
			Author:      article.Byline,
			Language:    article.Language,
			SourceURL:   sourceURL,
		},
		Tokens: models.TokenInfo{
			OriginalEstimate: originalTokens,
			CleanedEstimate:  cleanedTokens,
			SavingsPercent:   savingsPercent,
		},
		// Timing is intentionally left zero-valued.
		// The API handler layer computes end-to-end timing.
	}, nil
}
