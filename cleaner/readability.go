package cleaner

import (
	"log/slog"
	nurl "net/url"
	"strings"

	readability "github.com/go-shiori/go-readability"
)

// minContentLength is the minimum TextContent length (in characters) for
// readability output to be considered valid. Below this threshold we assume
// the algorithm failed to locate the main content and fall back to raw HTML.
const minContentLength = 50

// ExtractContent runs the Mozilla Readability algorithm on rawHTML.
//
// On success it returns the Article with clean HTML in Content, plain text in
// TextContent, and metadata (Title, Byline, Excerpt, SiteName, Language).
//
// Fallback behaviour (the API must never fail just because readability choked):
//   - If URL parsing fails          → return raw HTML in Content
//   - If readability.FromReader errs → return raw HTML in Content
//   - If extracted TextContent < 50  → return raw HTML in Content
//
// The caller can tell whether fallback was used by checking article.Title == "".
func ExtractContent(rawHTML string, sourceURL string) (readability.Article, bool) {
	parsedURL, err := nurl.Parse(sourceURL)
	if err != nil {
		slog.Warn("readability: invalid source URL, falling back to raw HTML",
			"url", sourceURL, "error", err,
		)
		return fallbackArticle(rawHTML), false
	}

	article, err := readability.FromReader(strings.NewReader(rawHTML), parsedURL)
	if err != nil {
		slog.Warn("readability: extraction failed, falling back to raw HTML",
			"url", sourceURL, "error", err,
		)
		return fallbackArticle(rawHTML), false
	}

	if len(strings.TrimSpace(article.TextContent)) < minContentLength {
		slog.Warn("readability: extracted content too short, falling back to raw HTML",
			"url", sourceURL, "length", len(article.TextContent),
		)
		return fallbackArticle(rawHTML), false
	}

	return article, true
}

// fallbackArticle wraps raw HTML into an Article so the pipeline can proceed
// uniformly regardless of whether readability succeeded.
func fallbackArticle(rawHTML string) readability.Article {
	return readability.Article{
		Content:     rawHTML,
		TextContent: rawHTML, // imperfect but ensures downstream never gets empty
	}
}
