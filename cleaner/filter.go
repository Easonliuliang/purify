package cleaner

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// FilterContent applies CSS-selector-based content filtering to raw HTML.
//
// Processing order:
//  1. Remove elements matching excludeTags (if any).
//  2. Keep only elements matching includeTags (if any).
//
// Returns the filtered HTML string. If both slices are empty, returns
// the input unchanged.
func FilterContent(html string, includeTags, excludeTags []string) string {
	if len(includeTags) == 0 && len(excludeTags) == 0 {
		return html
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return html
	}

	// Step 1: Remove excluded elements.
	for _, selector := range excludeTags {
		doc.Find(selector).Remove()
	}

	// Step 2: Keep only included elements.
	if len(includeTags) > 0 {
		// Build a combined selector: "article, main, .content"
		combined := strings.Join(includeTags, ", ")
		matches := doc.Find(combined)
		if matches.Length() > 0 {
			// Collect the outer HTML of all matching elements.
			var buf strings.Builder
			matches.Each(func(_ int, s *goquery.Selection) {
				h, err := goquery.OuterHtml(s)
				if err == nil {
					buf.WriteString(h)
				}
			})
			return buf.String()
		}
		// If no elements match the include selectors, return
		// the (already exclude-filtered) HTML as a fallback.
	}

	// Return the modified document HTML.
	result, err := doc.Html()
	if err != nil {
		return html
	}
	return result
}
