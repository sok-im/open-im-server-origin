package linkpreview

import (
	"net/url"
	"testing"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
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
	svc := NewService(Config{})
	cases := []struct {
		url       string
		shouldErr bool
	}{
		{"https://www.baidu.com", false},
		{"http://example.com", false},
		{"ftp://example.com", true},
		{"http://127.0.0.1", true},
		{"http://localhost", true},
		{"http://user:pass@example.com", true},
		{"http://example.com:8080", true},
		{"http://metadata.google.internal", true},
		{"", true},
	}

	for _, tc := range cases {
		_, err := svc.validateTargetURLString(tc.url)
		if tc.shouldErr && err == nil {
			t.Fatalf("expected error for %q", tc.url)
		}
		if !tc.shouldErr && err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.url, err)
		}
	}
}

func TestDomainWhitelist(t *testing.T) {
	svc := NewService(Config{AllowedDomains: []string{"baidu.com", "example.org"}})

	allowed := []string{
		"https://baidu.com",
		"https://www.baidu.com/path",
		"https://news.example.org",
	}
	for _, raw := range allowed {
		if _, err := svc.validateTargetURLString(raw); err != nil {
			t.Fatalf("expected allowed %q, got %v", raw, err)
		}
	}

	blocked := []string{
		"https://google.com",
		"https://evilbaidu.com",
	}
	for _, raw := range blocked {
		if _, err := svc.validateTargetURLString(raw); err == nil {
			t.Fatalf("expected blocked %q", raw)
		}
	}
}

func TestNormalizeCacheKey(t *testing.T) {
	u1, _ := url.Parse("https://Example.com/path/?a=1#frag")
	u2, _ := url.Parse("https://example.com/path?a=1")
	if normalizeCacheKey(u1) != normalizeCacheKey(u2) {
		t.Fatalf("cache keys should match: %q vs %q", normalizeCacheKey(u1), normalizeCacheKey(u2))
	}
}

func TestMatchAllowedDomain(t *testing.T) {
	allowed := normalizeDomains([]string{".baidu.com"})
	if !matchAllowedDomain("www.baidu.com", allowed) {
		t.Fatal("subdomain should match")
	}
	if matchAllowedDomain("notbaidu.com", allowed) {
		t.Fatal("unrelated domain should not match")
	}
}

func TestCacheStoresResult(t *testing.T) {
	svc := NewService(Config{
		CacheTTL:         time.Minute,
		NegativeCacheTTL: 0,
		CacheSize:        16,
	})
	key := "https://example.com/article"
	want := &apistruct.LinkPreviewResp{Title: "Cached Title", SiteName: "example.com"}
	svc.cache.Add(key, &cacheEntry{resp: cloneResp(want)})

	entry, ok := svc.cache.Get(key)
	if !ok || entry.resp.Title != want.Title {
		t.Fatalf("cache miss or wrong value: %+v", entry)
	}
}
