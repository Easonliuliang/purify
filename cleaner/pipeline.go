package cleaner

import (
	"log/slog"
	"math"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
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
	switch extractMode {
	case "raw":
		// Skip readability; use the full rendered HTML as-is.
		article = fallbackArticle(rawHTML)

	case "pruning":
		// Scoring-based content extraction.
		prunedHTML, err := PruneContent(rawHTML, sourceURL)
		if err != nil {
			slog.Warn("pruning: extraction failed, falling back to raw HTML",
				"url", sourceURL, "error", err,
			)
			prunedHTML = rawHTML
		}
		// Build an Article from pruned HTML. Metadata comes from
		// readability on the original HTML so we get title/author/etc.
		metaArticle, _ := ExtractContent(rawHTML, sourceURL)
		article = readability.Article{
			Title:       metaArticle.Title,
			Byline:      metaArticle.Byline,
			Excerpt:     metaArticle.Excerpt,
			SiteName:    metaArticle.SiteName,
			Language:    metaArticle.Language,
			Content:     prunedHTML,
			TextContent: stripTags(prunedHTML),
		}

	case "auto":
		// Run both readability and pruning concurrently, pick the
		// result with more extracted text content.
		article = autoExtract(rawHTML, sourceURL)

	default:
		// "readability" (default).
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

// autoExtract runs both Readability and Pruning concurrently, then picks the
// result that extracted more meaningful text content.
func autoExtract(rawHTML, sourceURL string) readability.Article {
	var (
		readabilityArticle readability.Article
		prunedHTML         string
		pruneErr           error
	)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		readabilityArticle, _ = ExtractContent(rawHTML, sourceURL)
	}()

	go func() {
		defer wg.Done()
		prunedHTML, pruneErr = PruneContent(rawHTML, sourceURL)
	}()

	wg.Wait()

	// If pruning failed, use readability result.
	if pruneErr != nil {
		slog.Warn("auto: pruning failed, using readability result",
			"url", sourceURL, "error", pruneErr,
		)
		return readabilityArticle
	}

	prunedText := stripTags(prunedHTML)
	readabilityText := strings.TrimSpace(readabilityArticle.TextContent)

	// Pick the result with more extracted text. If readability produced
	// very little (< minContentLength), prefer pruning, and vice versa.
	// When both are substantial, prefer whichever has more content.
	useReadability := len(readabilityText) >= len(prunedText)

	// Quality check: if the longer result is >10x the shorter, it may
	// contain too much noise — prefer the shorter one if it still has
	// a reasonable amount of content.
	if useReadability && len(prunedText) > minContentLength {
		if len(readabilityText) > 10*len(prunedText) {
			useReadability = false
		}
	} else if !useReadability && len(readabilityText) > minContentLength {
		if len(prunedText) > 10*len(readabilityText) {
			useReadability = true
		}
	}

	if useReadability {
		return readabilityArticle
	}

	// Build Article from pruned result, with metadata from readability.
	return readability.Article{
		Title:       readabilityArticle.Title,
		Byline:      readabilityArticle.Byline,
		Excerpt:     readabilityArticle.Excerpt,
		SiteName:    readabilityArticle.SiteName,
		Language:    readabilityArticle.Language,
		Content:     prunedHTML,
		TextContent: prunedText,
	}
}

// stripTags is a simple helper that extracts visible text from an HTML
// fragment by parsing it with goquery. Returns trimmed plain text.
func stripTags(html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return html
	}
	return strings.TrimSpace(doc.Text())
}
