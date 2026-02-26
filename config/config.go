package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all application configuration.
type Config struct {
	Server    ServerConfig
	Browser   BrowserConfig
	Scraper   ScraperConfig
	Auth      AuthConfig
	RateLimit RateLimitConfig
	Log       LogConfig
}

// ServerConfig controls the HTTP server.
type ServerConfig struct {
	Host string // default: "0.0.0.0"
	Port int    // default: 8080
	Mode string // "debug", "release", "test"; default: "release"
}

// BrowserConfig controls the Rod browser instance.
type BrowserConfig struct {
	// Headless controls whether the browser runs headless.
	Headless bool // default: true

	// MaxPages is the page pool capacity (max concurrent tabs).
	MaxPages int // default: 10

	// DefaultProxy is the default proxy URL for all requests.
	DefaultProxy string

	// NoSandbox disables Chrome's sandbox (needed in Docker).
	NoSandbox bool // default: false

	// BrowserBin overrides the Chromium binary path.
	BrowserBin string
}

// ScraperConfig controls scraping behavior.
type ScraperConfig struct {
	// DefaultTimeout is the per-request timeout.
	DefaultTimeout time.Duration // default: 30s

	// MaxTimeout is the maximum allowed timeout from the client.
	MaxTimeout time.Duration // default: 120s

	// NavigationTimeout is the max time for page.Navigate alone.
	NavigationTimeout time.Duration // default: 15s

	// BlockedResourceTypes lists resource types to block.
	// default: ["Image", "Stylesheet", "Font", "Media"]
	BlockedResourceTypes []string
}

// AuthConfig controls API key authentication.
type AuthConfig struct {
	// Enabled toggles API key authentication.
	Enabled bool // default: true

	// APIKeys is the list of valid API keys (for MVP; replace with DB later).
	APIKeys []string
}

// RateLimitConfig controls per-key rate limiting.
type RateLimitConfig struct {
	// RequestsPerSecond is the sustained rate per API key.
	RequestsPerSecond float64 // default: 5

	// Burst is the maximum burst size per API key.
	Burst int // default: 10
}

// LogConfig controls structured logging.
type LogConfig struct {
	Level  string // default: "info"
	Format string // "json" or "text"; default: "json"
}

// Load reads configuration from environment variables with sane defaults.
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Host: envOr("PURIFY_HOST", "0.0.0.0"),
			Port: envIntOr("PURIFY_PORT", 8080),
			Mode: envOr("PURIFY_MODE", "release"),
		},
		Browser: BrowserConfig{
			Headless:     envBoolOr("PURIFY_HEADLESS", true),
			MaxPages:     envIntOr("PURIFY_MAX_PAGES", 10),
			DefaultProxy: os.Getenv("PURIFY_PROXY"),
			NoSandbox:    envBoolOr("PURIFY_NO_SANDBOX", false),
			BrowserBin:   os.Getenv("PURIFY_BROWSER_BIN"),
		},
		Scraper: ScraperConfig{
			DefaultTimeout:    envDurationOr("PURIFY_DEFAULT_TIMEOUT", 30*time.Second),
			MaxTimeout:        envDurationOr("PURIFY_MAX_TIMEOUT", 120*time.Second),
			NavigationTimeout: envDurationOr("PURIFY_NAV_TIMEOUT", 15*time.Second),
			BlockedResourceTypes: envSliceOr("PURIFY_BLOCKED_RESOURCES", []string{
				"Image", "Stylesheet", "Font", "Media",
			}),
		},
		Auth: AuthConfig{
			Enabled: envBoolOr("PURIFY_AUTH_ENABLED", true),
			APIKeys: envSliceOr("PURIFY_API_KEYS", nil),
		},
		RateLimit: RateLimitConfig{
			RequestsPerSecond: envFloatOr("PURIFY_RATE_RPS", 5.0),
			Burst:             envIntOr("PURIFY_RATE_BURST", 10),
		},
		Log: LogConfig{
			Level:  envOr("PURIFY_LOG_LEVEL", "info"),
			Format: envOr("PURIFY_LOG_FORMAT", "json"),
		},
	}
}

// --- helper functions ---

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envBoolOr(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envFloatOr(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func envDurationOr(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func envSliceOr(key string, fallback []string) []string {
	if v := os.Getenv(key); v != "" {
		parts := strings.Split(v, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	}
	return fallback
}
