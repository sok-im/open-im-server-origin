package linkpreview

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/openimsdk/open-im-server/v3/pkg/apistruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/tools/errs"
)

const (
	maxBodySize  = 2 << 20 // 2 MiB
	fetchTimeout = 10 * time.Second
	userAgent    = "Mozilla/5.0 (compatible; OpenIMLinkPreview/1.0)"
	maxRedirects = 5
)

type cacheEntry struct {
	resp *apistruct.LinkPreviewResp
	err  error
}

type Service struct {
	cfg        Config
	allowed    []string
	httpClient *http.Client
	cache      *expirable.LRU[string, *cacheEntry]
	negCache   *expirable.LRU[string, *cacheEntry]
}

func NewService(cfg Config) *Service {
	cfg = cfg.withDefaults()
	s := &Service{
		cfg:     cfg,
		allowed: normalizeDomains(cfg.AllowedDomains),
	}
	s.httpClient = &http.Client{
		Timeout: fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("too many redirects")
			}
			return s.validateTargetURL(req.URL)
		},
		Transport: &http.Transport{
			DialContext: s.dialContext,
		},
	}
	if cfg.CacheTTL > 0 {
		s.cache = expirable.NewLRU[string, *cacheEntry](cfg.CacheSize, nil, cfg.CacheTTL)
	}
	if cfg.NegativeCacheTTL > 0 {
		s.negCache = expirable.NewLRU[string, *cacheEntry](cfg.CacheSize, nil, cfg.NegativeCacheTTL)
	}
	return s
}

var defaultService = NewService(Config{})

// Fetch uses the default service instance.
func Fetch(ctx context.Context, rawURL string) (*apistruct.LinkPreviewResp, error) {
	return defaultService.Fetch(ctx, rawURL)
}

func (s *Service) Fetch(ctx context.Context, rawURL string) (*apistruct.LinkPreviewResp, error) {
	parsed, err := s.validateTargetURLString(rawURL)
	if err != nil {
		return nil, err
	}
	cacheKey := normalizeCacheKey(parsed)

	if s.cache != nil {
		if entry, ok := s.cache.Get(cacheKey); ok && entry.err == nil {
			return cloneResp(entry.resp), nil
		}
	}
	if s.negCache != nil {
		if entry, ok := s.negCache.Get(cacheKey); ok && entry.err != nil {
			return nil, entry.err
		}
	}

	resp, err := s.fetchRemote(ctx, parsed)
	if err != nil {
		if s.negCache != nil && isCacheableError(err) {
			s.negCache.Add(cacheKey, &cacheEntry{err: err})
		}
		return nil, err
	}
	if s.cache != nil {
		s.cache.Add(cacheKey, &cacheEntry{resp: cloneResp(resp)})
	}
	return resp, nil
}

func (s *Service) fetchRemote(ctx context.Context, parsed *url.URL) (*apistruct.LinkPreviewResp, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, errs.ErrArgs.WrapMsg(err.Error())
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := s.httpClient.Do(req)
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

func (s *Service) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
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
}

func (s *Service) validateTargetURLString(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errs.ErrArgs.WrapMsg("url is empty")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, errs.ErrArgs.WrapMsg("invalid url")
	}
	if err := s.validateTargetURL(parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func (s *Service) validateTargetURL(u *url.URL) error {
	if u == nil {
		return errs.ErrArgs.WrapMsg("invalid url")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return errs.ErrArgs.WrapMsg("only http and https urls are supported")
	}
	if u.User != nil {
		return errs.ErrArgs.WrapMsg("url must not contain user credentials")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return errs.ErrArgs.WrapMsg("invalid url host")
	}
	if err := validateBlockedHost(host); err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return errs.ErrArgs.WrapMsg("private address is not allowed")
		}
	} else if !matchAllowedDomain(host, s.allowed) {
		return errs.ErrArgs.WrapMsg("domain is not in the allowed list")
	}
	if err := validatePort(u); err != nil {
		return err
	}
	return nil
}

func validateBlockedHost(host string) error {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errs.ErrArgs.WrapMsg("localhost is not allowed")
	}
	blockedHosts := []string{
		"metadata.google.internal",
		"metadata.goog",
	}
	for _, blocked := range blockedHosts {
		if host == blocked {
			return errs.ErrArgs.WrapMsg("target host is not allowed")
		}
	}
	blockedSuffixes := []string{".local", ".internal", ".lan", ".corp", ".home"}
	for _, suffix := range blockedSuffixes {
		if strings.HasSuffix(host, suffix) {
			return errs.ErrArgs.WrapMsg("target host is not allowed")
		}
	}
	return nil
}

func validatePort(u *url.URL) error {
	port := u.Port()
	if port == "" {
		return nil
	}
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		return nil
	}
	return errs.ErrArgs.WrapMsg("only port 80 and 443 are allowed")
}

func normalizeDomains(domains []string) []string {
	if len(domains) == 0 {
		return nil
	}
	out := make([]string, 0, len(domains))
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		domain = strings.TrimPrefix(domain, ".")
		if domain != "" {
			out = append(out, domain)
		}
	}
	return out
}

func matchAllowedDomain(host string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, domain := range allowed {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func normalizeCacheKey(u *url.URL) string {
	if u == nil {
		return ""
	}
	clone := *u
	clone.Fragment = ""
	clone.Host = strings.ToLower(clone.Hostname())
	if port := u.Port(); port != "" {
		if !((clone.Scheme == "http" && port == "80") || (clone.Scheme == "https" && port == "443")) {
			clone.Host = net.JoinHostPort(clone.Hostname(), port)
		}
	}
	if clone.Path != "/" && strings.HasSuffix(clone.Path, "/") {
		clone.Path = strings.TrimSuffix(clone.Path, "/")
	}
	return clone.String()
}

func isCacheableError(err error) bool {
	if err == nil {
		return false
	}
	if errs.ErrArgs.Is(err) {
		return false
	}
	return true
}

func cloneResp(resp *apistruct.LinkPreviewResp) *apistruct.LinkPreviewResp {
	if resp == nil {
		return nil
	}
	cp := *resp
	return &cp
}

var privateIPBlocks []*net.IPNet

func init() {
	for _, cidr := range []string{
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"192.168.0.0/16",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4",
		"240.0.0.0/4",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	} {
		_, block, err := net.ParseCIDR(cidr)
		if err == nil {
			privateIPBlocks = append(privateIPBlocks, block)
		}
	}
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// SetDefaultService replaces the package-level service (mainly for tests).
func SetDefaultService(s *Service) {
	if s == nil {
		return
	}
	defaultServiceMu.Lock()
	defaultService = s
	defaultServiceMu.Unlock()
}

var defaultServiceMu sync.RWMutex
