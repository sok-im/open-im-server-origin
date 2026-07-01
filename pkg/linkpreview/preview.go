package linkpreview

import (
	"net/url"
	"strings"

	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
	"golang.org/x/net/html"
)

func parseHTML(content string, pageURL *url.URL) *apistruct.LinkPreviewResp {
	meta := extractMeta(content)
	title := firstNonEmpty(
		meta["og:title"],
		meta["twitter:title"],
		extractTitleTag(content),
	)
	description := firstNonEmpty(
		meta["og:description"],
		meta["twitter:description"],
		meta["description"],
	)
	imageURL := firstNonEmpty(
		meta["og:image"],
		meta["twitter:image"],
		meta["twitter:image:src"],
	)
	siteName := firstNonEmpty(
		meta["og:site_name"],
		pageURL.Hostname(),
	)

	return &apistruct.LinkPreviewResp{
		Title:       title,
		Description: description,
		ImageURL:    resolveURL(pageURL, imageURL),
		SiteName:    siteName,
	}
}

func extractMeta(content string) map[string]string {
	result := make(map[string]string)
	tokenizer := html.NewTokenizer(strings.NewReader(content))
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return result
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.Data != "meta" {
				continue
			}
			var property, name, contentVal string
			for _, attr := range token.Attr {
				switch strings.ToLower(attr.Key) {
				case "property":
					property = strings.TrimSpace(attr.Val)
				case "name":
					name = strings.TrimSpace(attr.Val)
				case "content":
					contentVal = strings.TrimSpace(attr.Val)
				}
			}
			if contentVal == "" {
				continue
			}
			key := strings.ToLower(firstNonEmpty(property, name))
			if key == "" {
				continue
			}
			if _, exists := result[key]; !exists {
				result[key] = contentVal
			}
		}
	}
}

func extractTitleTag(content string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(content))
	inTitle := false
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return ""
		case html.StartTagToken:
			if tokenizer.Token().Data == "title" {
				inTitle = true
			}
		case html.TextToken:
			if inTitle {
				return strings.TrimSpace(tokenizer.Token().Data)
			}
		case html.EndTagToken:
			if tokenizer.Token().Data == "title" {
				return ""
			}
		}
	}
}

func resolveURL(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || base == nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
