package cleaner

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/use-agent/purify/models"
)

// ExtractLinks parses the raw HTML and separates links into internal and external
// based on whether their host matches the source URL's host.
func ExtractLinks(rawHTML string, sourceURL string) models.LinksResult {
	result := models.LinksResult{
		Internal: []models.Link{},
		External: []models.Link{},
	}

	base, err := url.Parse(sourceURL)
	if err != nil {
		return result
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return result
	}

	seen := make(map[string]struct{})
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists || href == "" {
			return
		}

		// Resolve relative URLs against the base.
		resolved, err := base.Parse(href)
		if err != nil {
			return
		}

		absURL := resolved.String()
		// Skip fragments, javascript:, mailto:, tel: etc.
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			return
		}

		// Deduplicate.
		if _, ok := seen[absURL]; ok {
			return
		}
		seen[absURL] = struct{}{}

		text := strings.TrimSpace(s.Text())
		link := models.Link{Href: absURL, Text: text}

		if strings.EqualFold(resolved.Host, base.Host) {
			result.Internal = append(result.Internal, link)
		} else {
			result.External = append(result.External, link)
		}
	})

	return result
}

// ExtractImages parses the raw HTML and returns image elements with absolute URLs.
func ExtractImages(rawHTML string, sourceURL string) []models.Image {
	images := []models.Image{}

	base, err := url.Parse(sourceURL)
	if err != nil {
		return images
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return images
	}

	seen := make(map[string]struct{})
	doc.Find("img[src]").Each(func(_ int, s *goquery.Selection) {
		src, exists := s.Attr("src")
		if !exists || src == "" {
			return
		}

		// Resolve relative URLs.
		resolved, err := base.Parse(src)
		if err != nil {
			return
		}

		absURL := resolved.String()
		// Skip data URIs.
		if resolved.Scheme == "data" {
			return
		}

		if _, ok := seen[absURL]; ok {
			return
		}
		seen[absURL] = struct{}{}

		alt, _ := s.Attr("alt")
		images = append(images, models.Image{
			Src: absURL,
			Alt: strings.TrimSpace(alt),
		})
	})

	return images
}

// ExtractOGMetadata parses Open Graph meta tags from the raw HTML.
func ExtractOGMetadata(rawHTML string) models.OGMetadata {
	og := models.OGMetadata{}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return og
	}

	doc.Find("meta[property]").Each(func(_ int, s *goquery.Selection) {
		prop, _ := s.Attr("property")
		content, _ := s.Attr("content")
		if content == "" {
			return
		}
		switch prop {
		case "og:title":
			og.Title = content
		case "og:description":
			og.Description = content
		case "og:image":
			og.Image = content
		case "og:type":
			og.Type = content
		}
	})

	return og
}
