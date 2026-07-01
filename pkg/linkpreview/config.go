package linkpreview

import "time"

const (
	defaultCacheTTL         = time.Hour
	defaultNegativeCacheTTL = 5 * time.Minute
	defaultCacheSize        = 4096
)

// Config controls SSRF checks, domain whitelist and preview cache.
type Config struct {
	// AllowedDomains 域名白名单；为空时不限制域名（仍受 SSRF 规则约束）。
	AllowedDomains []string
	// CacheTTL 成功预览缓存时长；<=0 表示不缓存。
	CacheTTL time.Duration
	// NegativeCacheTTL 抓取失败缓存时长，避免对不可用 URL 重复请求；<=0 表示不缓存失败。
	NegativeCacheTTL time.Duration
	// CacheSize 内存缓存条目上限。
	CacheSize int
}

func (c Config) withDefaults() Config {
	cfg := c
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = defaultCacheTTL
	} else if cfg.CacheTTL < 0 {
		cfg.CacheTTL = 0
	}
	if cfg.NegativeCacheTTL == 0 {
		cfg.NegativeCacheTTL = defaultNegativeCacheTTL
	} else if cfg.NegativeCacheTTL < 0 {
		cfg.NegativeCacheTTL = 0
	}
	if cfg.CacheSize <= 0 {
		cfg.CacheSize = defaultCacheSize
	}
	return cfg
}

// ConfigFromSeconds builds Config from second-based settings (used by YAML config).
// cacheTTL / negativeCacheTTL: >0 explicit seconds, 0 = package default, <0 = disabled.
func ConfigFromSeconds(allowedDomains []string, cacheTTL, negativeCacheTTL, cacheSize int) Config {
	cfg := Config{
		AllowedDomains: allowedDomains,
		CacheSize:      cacheSize,
	}
	switch {
	case cacheTTL > 0:
		cfg.CacheTTL = time.Duration(cacheTTL) * time.Second
	case cacheTTL < 0:
		cfg.CacheTTL = -1
	default:
		cfg.CacheTTL = 0
	}
	switch {
	case negativeCacheTTL > 0:
		cfg.NegativeCacheTTL = time.Duration(negativeCacheTTL) * time.Second
	case negativeCacheTTL < 0:
		cfg.NegativeCacheTTL = -1
	default:
		cfg.NegativeCacheTTL = 0
	}
	return cfg.withDefaults()
}
