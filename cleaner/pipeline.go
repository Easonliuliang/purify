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

// CleanOptions carries optional content-filtering parameters for the pipeline.
type CleanOptions struct {
	IncludeTags []string
	ExcludeTags []string
}

// Clean runs the full pipeline and returns a partial ScrapeResponse
// (Content + Metadata + Tokens filled; Timing is left to the API layer).
//
// Flow:
//  1. Estimate original tokens from raw HTML.
//  1b. Apply include/exclude tag filters (if provided).
//  2. Stage 1: go-readability extracts main content.
//     Fallback: if extraction fails or content is too short, use raw HTML.
//  3. Stage 2: convert to the requested output format.
//  4. Estimate cleaned tokens and compute savings.
//  5. Assemble and return the partial response.
func (c *Cleaner) Clean(rawHTML string, sourceURL string, format string, extractMode string, opts ...CleanOptions) (*models.ScrapeResponse, error) {
	// ── 1. Original token estimate ──────────────────────────────────
	originalTokens := EstimateTokens(rawHTML)

	// ── 1b. Content filtering (include/exclude tags) ────────────────
	if len(opts) > 0 {
		o := opts[0]
		rawHTML = FilterContent(rawHTML, o.IncludeTags, o.ExcludeTags)
	}

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

	// ── 5. Extract links, images, OG metadata from raw HTML ────────
	links := ExtractLinks(rawHTML, sourceURL)
	images := ExtractImages(rawHTML, sourceURL)
	ogMeta := ExtractOGMetadata(rawHTML)

	// ── 6. Assemble partial response ────────────────────────────────
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
		Links:      links,
		Images:     images,
		OGMetadata: ogMeta,
		Tokens: models.TokenInfo{
			OriginalEstimate: originalTokens,
			CleanedEstimate:  cleanedTokens,
			SavingsPercent:   savingsPercent,
		},
		// Timing, StatusCode, FinalURL are left zero-valued.
		// The API handler layer fills them in.
	}, nil
}
