package linkpreview

import (
	"net/url"
	"testing"
)

func TestParseHTML_OpenGraph(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
  <meta property="og:title" content="Baidu" />
  <meta property="og:description" content="百度一下，你就知道..." />
  <meta property="og:image" content="https://www.baidu.com/img/flexible/logo/pc/result.png" />
  <meta property="og:site_name" content="Baidu" />
  <title>fallback title</title>
</head>
<body></body>
</html>`

	pageURL, _ := url.Parse("https://www.baidu.com/")
	resp := parseHTML(html, pageURL)

	if resp.Title != "Baidu" {
		t.Fatalf("title = %q, want Baidu", resp.Title)
	}
	if resp.Description != "百度一下，你就知道..." {
		t.Fatalf("description = %q", resp.Description)
	}
	if resp.ImageURL != "https://www.baidu.com/img/flexible/logo/pc/result.png" {
		t.Fatalf("imageUrl = %q", resp.ImageURL)
	}
	if resp.SiteName != "Baidu" {
		t.Fatalf("siteName = %q, want Baidu", resp.SiteName)
	}
}

func TestParseHTML_FallbackMeta(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
  <meta name="description" content="Example description" />
  <title>Example Title</title>
</head>
<body></body>
</html>`

	pageURL, _ := url.Parse("https://example.com/page")
	resp := parseHTML(html, pageURL)

	if resp.Title != "Example Title" {
		t.Fatalf("title = %q", resp.Title)
	}
	if resp.Description != "Example description" {
		t.Fatalf("description = %q", resp.Description)
	}
	if resp.SiteName != "example.com" {
		t.Fatalf("siteName = %q", resp.SiteName)
	}
}

func TestParseHTML_RelativeImageURL(t *testing.T) {
	html := `<html><head><meta property="og:image" content="/images/logo.png" /></head></html>`
	pageURL, _ := url.Parse("https://example.com/page")
	resp := parseHTML(html, pageURL)
	if resp.ImageURL != "https://example.com/images/logo.png" {
		t.Fatalf("imageUrl = %q", resp.ImageURL)
	}
}

func TestValidateTargetURLString(t *testing.T) {
	cases := []struct {
		url       string
		shouldErr bool
	}{
		{"https://www.baidu.com", false},
		{"http://example.com", false},
		{"ftp://example.com", true},
		{"http://127.0.0.1", true},
		{"http://localhost", true},
		{"", true},
	}

	for _, tc := range cases {
		_, err := validateTargetURLString(tc.url)
		if tc.shouldErr && err == nil {
			t.Fatalf("expected error for %q", tc.url)
		}
		if !tc.shouldErr && err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.url, err)
		}
	}
}
