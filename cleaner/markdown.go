package cleaner

import (
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
)

// newMarkdownConverter creates a reusable, goroutine-safe Converter configured
// for LLM-optimised output:
//
//   - base plugin: strips script, style, iframe, noscript, head, meta, link,
//     input, textarea, HTML comments — all noise for LLMs.
//   - commonmark plugin: standard Markdown rendering (headings, lists, links,
//     code blocks, emphasis, blockquotes, etc.).
//   - table plugin: preserves table structure (critical for LLM comprehension
//     of tabular data) with minimal cell padding to save tokens.
func newMarkdownConverter() *converter.Converter {
	return converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			table.NewTablePlugin(
				// "minimal" adds a single space padding per cell instead of
				// aligning all columns to equal width. This can save 20-40%
				// of table-related tokens while remaining perfectly readable.
				table.WithCellPaddingBehavior(table.CellPaddingBehaviorMinimal),
			),
		),
	)
}

// ToMarkdown converts clean HTML to Markdown using html-to-markdown v2.
//
// The domain parameter is used to resolve relative URLs in <a> and <img> tags
// into absolute URLs, so the Markdown output is self-contained.
func ToMarkdown(conv *converter.Converter, htmlContent string, domain string) (string, error) {
	return conv.ConvertString(htmlContent, converter.WithDomain(domain))
}
