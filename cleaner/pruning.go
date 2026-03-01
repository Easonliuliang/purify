package cleaner

import (
	"math"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// pruneScoreThreshold is the minimum weighted score a block element must reach
// to be retained as main content. Blocks scoring at or below this value are
// discarded as boilerplate (navigation, sidebars, footers, ads, etc.).
const pruneScoreThreshold = 0.0

// Signal weights for the pruning scorer.
const (
	wTextDensity   = 3.0
	wLinkDensity   = -2.0
	wTagWeight     = 1.5
	wClassIDWeight = 1.0
	wTextLength    = 0.5
)

// positiveClassIDPatterns are substrings in class/id attributes that indicate
// main content areas.
var positiveClassIDPatterns = []string{
	"content", "article", "post", "entry", "body", "main", "text",
}

// negativeClassIDPatterns are substrings in class/id attributes that indicate
// non-content areas (boilerplate).
var negativeClassIDPatterns = []string{
	"sidebar", "ad", "widget", "nav", "menu", "comment", "footer",
	"header", "banner", "popup", "modal", "cookie", "social", "share",
	"related", "recommend", "promo",
}

// PruneContent extracts main content from raw HTML using a scoring-based
// approach. Each top-level block element in <body> is scored based on text
// density, link density, semantic tag weight, class/id signals, and text
// length. Only blocks exceeding the threshold are retained.
//
// If no blocks pass the threshold, the full body content is returned as a
// fallback so the pipeline never produces empty output.
func PruneContent(rawHTML, sourceURL string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return rawHTML, err
	}

	body := doc.Find("body")
	if body.Length() == 0 {
		// No <body> tag — return raw HTML unchanged.
		return rawHTML, nil
	}

	var retained []string
	body.Children().Each(func(_ int, el *goquery.Selection) {
		score := scoreElement(el)
		if score > pruneScoreThreshold {
			if html, err := goquery.OuterHtml(el); err == nil {
				retained = append(retained, html)
			}
		}
	})

	// Fallback: if nothing passed the threshold, return full body content.
	if len(retained) == 0 {
		html, err := body.Html()
		if err != nil {
			return rawHTML, nil
		}
		return html, nil
	}

	return strings.Join(retained, "\n"), nil
}

// scoreElement computes a weighted score for a DOM element based on multiple
// content signals.
func scoreElement(el *goquery.Selection) float64 {
	fullHTML, err := goquery.OuterHtml(el)
	if err != nil {
		return 0
	}

	text := strings.TrimSpace(el.Text())
	textLen := len(text)
	totalLen := len(fullHTML)

	// --- text_density: ratio of visible text to total element size ---
	textDensity := 0.0
	if totalLen > 0 {
		textDensity = float64(textLen) / float64(totalLen)
	}

	// --- link_density: ratio of anchor text to total text ---
	linkTextLen := 0
	el.Find("a").Each(func(_ int, a *goquery.Selection) {
		linkTextLen += len(strings.TrimSpace(a.Text()))
	})
	linkDensity := 0.0
	if textLen > 0 {
		linkDensity = float64(linkTextLen) / float64(textLen)
	}

	// --- tag_weight: semantic tag bonus/penalty ---
	tagW := tagWeight(el)

	// --- class_id_weight: class/id attribute bonus/penalty ---
	classIDW := classIDWeight(el)

	// --- text_length: log-scale bonus for longer text blocks ---
	textLenScore := math.Log10(float64(textLen) + 1)

	score := textDensity*wTextDensity +
		linkDensity*wLinkDensity +
		tagW*wTagWeight +
		classIDW*wClassIDWeight +
		textLenScore*wTextLength

	return score
}

// tagWeight returns a score bonus/penalty based on the element's tag name.
// Semantic content tags get a positive boost; known boilerplate tags get a
// negative penalty.
func tagWeight(el *goquery.Selection) float64 {
	tag := goquery.NodeName(el)
	switch tag {
	case "article", "main", "section":
		return 5.0
	case "nav", "footer", "aside", "header":
		return -5.0
	default:
		return 0.0
	}
}

// classIDWeight scans the element's class and id attributes for substrings
// that indicate content vs. boilerplate.
func classIDWeight(el *goquery.Selection) float64 {
	class, _ := el.Attr("class")
	id, _ := el.Attr("id")
	combined := strings.ToLower(class + " " + id)

	score := 0.0
	for _, pat := range positiveClassIDPatterns {
		if strings.Contains(combined, pat) {
			score += 3.0
			break // count at most once per direction
		}
	}
	for _, pat := range negativeClassIDPatterns {
		if strings.Contains(combined, pat) {
			score -= 3.0
			break
		}
	}
	return score
}
