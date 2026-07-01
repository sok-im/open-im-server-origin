package linkpreview

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/tools/errs"
	"golang.org/x/net/html"
)

const (
	maxBodySize   = 2 << 20 // 2 MiB
	fetchTimeout  = 10 * time.Second
	userAgent     = "Mozilla/5.0 (compatible; OpenIMLinkPreview/1.0)"
	maxRedirects  = 5
)

var httpClient = &http.Client{
	Timeout: fetchTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("too many redirects")
		}
		if err := validateTargetURL(req.URL); err != nil {
			return err
		}
		return nil
	},
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if isPrivateIP(ip.IP) {
					return nil, fmt.Errorf("private address is not allowed")
				}
			}
			dialer := &net.Dialer{Timeout: fetchTimeout}
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		},
	},
}

func Fetch(ctx context.Context, rawURL string) (*apistruct.LinkPreviewResp, error) {
	parsed, err := validateTargetURLString(rawURL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, errs.ErrArgs.WrapMsg(err.Error())
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, servererrs.ErrNetwork.WrapMsg(err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, servererrs.ErrNetwork.WrapMsg(fmt.Sprintf("unexpected status code: %d", resp.StatusCode))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, servererrs.ErrNetwork.WrapMsg(err.Error())
	}

	finalURL := parsed
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL
	}

	return parseHTML(string(body), finalURL), nil
}

func validateTargetURLString(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errs.ErrArgs.WrapMsg("url is empty")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, errs.ErrArgs.WrapMsg("invalid url")
	}
	if err := validateTargetURL(parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func validateTargetURL(u *url.URL) error {
	if u == nil {
		return errs.ErrArgs.WrapMsg("invalid url")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return errs.ErrArgs.WrapMsg("only http and https urls are supported")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return errs.ErrArgs.WrapMsg("invalid url host")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errs.ErrArgs.WrapMsg("localhost is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil && isPrivateIP(ip) {
		return errs.ErrArgs.WrapMsg("private address is not allowed")
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	return false
}

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
