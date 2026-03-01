package simhash

import (
	"strings"

	"golang.org/x/net/html"
)

// FingerprintDOM computes a SimHash fingerprint of the DOM structure.
// Only considers tag names in sequence, ignoring text content, attributes, etc.
// Useful for comparing if two HTML documents have similar structure
// (e.g., HTTP-fetched vs JS-rendered versions).
func FingerprintDOM(htmlStr string) uint64 {
	tags := extractTags(htmlStr)
	if len(tags) == 0 {
		return 0
	}

	shingles := makeShingles(tags, 3)
	if len(shingles) == 0 {
		// Fall back to the tag sequence itself if too few tags for shingles.
		return Fingerprint(strings.Join(tags, " "))
	}

	return Fingerprint(strings.Join(shingles, " "))
}

// extractTags walks HTML with the tokenizer and collects open tag names in order.
func extractTags(htmlStr string) []string {
	tokenizer := html.NewTokenizer(strings.NewReader(htmlStr))
	var tags []string

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return tags
		case html.StartTagToken, html.SelfClosingTagToken:
			tn, _ := tokenizer.TagName()
			tags = append(tags, string(tn))
		}
	}
}

// makeShingles creates n-gram shingles from a slice of tokens.
func makeShingles(tokens []string, n int) []string {
	if len(tokens) < n {
		return nil
	}

	shingles := make([]string, 0, len(tokens)-n+1)
	for i := 0; i <= len(tokens)-n; i++ {
		shingles = append(shingles, strings.Join(tokens[i:i+n], "_"))
	}
	return shingles
}
