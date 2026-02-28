package cleaner

import (
	"bytes"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// ApplyCSSSelector parses rawHTML, matches elements against the given CSS
// selector, and returns the concatenated outer HTML of all matched elements.
//
// If no elements match, the original rawHTML is returned unchanged so that
// downstream processing still has something to work with.
func ApplyCSSSelector(rawHTML string, selector string) (string, error) {
	sel, err := cascadia.Parse(selector)
	if err != nil {
		return "", err
	}

	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return "", err
	}

	matches := cascadia.QueryAll(doc, sel)
	if len(matches) == 0 {
		// No matches — fall back to original HTML to avoid empty output.
		return rawHTML, nil
	}

	var buf bytes.Buffer
	for _, node := range matches {
		if err := html.Render(&buf, node); err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}
