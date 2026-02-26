package cleaner

import "unicode/utf8"

// EstimateTokens provides a fast token count estimate without importing tiktoken.
//
// Heuristic: utf8 rune count / 3.
//
//   - English text averages ~4 chars/token, CJK text averages ~1.5 chars/token.
//   - Dividing by 3 is a reasonable middle-ground for mixed-language content.
//   - This intentionally over-estimates slightly (conservative), which is safer
//     for showing savings ratios — users see a genuine improvement, never inflated.
func EstimateTokens(text string) int {
	n := utf8.RuneCountInString(text)
	if n == 0 {
		return 0
	}
	est := n / 3
	if est < 1 {
		return 1
	}
	return est
}
