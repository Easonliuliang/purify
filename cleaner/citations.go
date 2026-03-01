package cleaner

import (
	"fmt"
	"regexp"
	"strings"
)

// inlineLinkRe matches Markdown inline links: [text](url)
var inlineLinkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

// ConvertToCitations converts inline Markdown links to reference-style citations.
//
// Input:  "See [Google](https://google.com) and [GitHub](https://github.com)"
// Output: "See [Google][1] and [GitHub][2]\n\n---\n[1]: https://google.com\n[2]: https://github.com"
//
// Duplicate URLs reuse the same reference number.
func ConvertToCitations(markdown string) string {
	urlToNum := make(map[string]int)
	var refs []string
	counter := 0

	result := inlineLinkRe.ReplaceAllStringFunc(markdown, func(match string) string {
		parts := inlineLinkRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		text := parts[1]
		url := parts[2]

		num, exists := urlToNum[url]
		if !exists {
			counter++
			num = counter
			urlToNum[url] = num
			refs = append(refs, fmt.Sprintf("[%d]: %s", num, url))
		}

		return fmt.Sprintf("[%s][%d]", text, num)
	})

	if len(refs) == 0 {
		return markdown
	}

	return result + "\n\n---\n" + strings.Join(refs, "\n")
}
