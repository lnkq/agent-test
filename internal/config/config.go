// Package config loads and validates the gateway configuration from a YAML file.
//
// The config is the single source of truth for routing, rate-limit profiles,
// upstreams and timeouts. Loading fails closed: any parse or validation error
// is returned to the caller, and a config that fails validation never becomes
// active.
package config

import (
	"fmt"
	"net/url"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration object.
type Config struct {
	Server     ServerConfig         `yaml:"server"`
	RateLimits map[string]RateLimit `yaml:"rate_limits"`
	Routes     []Route              `yaml:"routes"`
}

// ServerConfig holds process-level settings.
type ServerConfig struct {
	// Listen is the address the gateway binds to, e.g. ":8080".
	Listen string `yaml:"listen"`
}

// Route maps a URL prefix to a weighted set of upstreams. The same Upstream
// list is the single mechanism for load balancing and canary rollouts.
type Route struct {
	Path      string     `yaml:"path"`
	Upstreams []Upstream `yaml:"upstreams"`
	// Limit references a named profile in Config.RateLimits, if any.
	Limit string `yaml:"limit"`
	// Timeout bounds how long a proxied request to an upstream may take.
	Timeout Duration `yaml:"timeout"`
}

// Upstream is a single backend and its relative weight.
type Upstream struct {
	URL    string `yaml:"url"`
	Weight int    `yaml:"weight"`
}

// RateLimit configures a token bucket limiter.
type RateLimit struct {
	Rate  float64 `yaml:"rate"`
	Burst int     `yaml:"burst"`
}

// Duration wraps time.Duration so config values can be written as "5s" or "2m".
type Duration time.Duration

// UnmarshalYAML parses a duration string such as "5s".
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(v)
	return nil
}

// DefaultListen is used when no server.listen is configured.
const DefaultListen = ":8080"

// Load reads the file at path and parses it into a validated Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	return Parse(data)
}

// Parse decodes and validates config bytes into a Config.
func Parse(data []byte) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := c.applyDefaultsAndValidate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaultsAndValidate() error {
	if c.Server.Listen == "" {
		c.Server.Listen = DefaultListen
	}

	// Apply defaults and validate rate-limit profiles. We work on the map
	// directly because map values cannot be addressed.
	for name, rl := range c.RateLimits {
		if rl.Rate <= 0 {
			return fmt.Errorf("rate limit %q: rate must be > 0", name)
		}
		if rl.Burst < 1 {
			return fmt.Errorf("rate limit %q: burst must be >= 1", name)
		}
	}

	for i := range c.Routes {
		r := &c.Routes[i]
		if r.Path == "" || r.Path[0] != '/' {
			return fmt.Errorf("route %d: path must be non-empty and start with '/'", i)
		}
		if len(r.Upstreams) == 0 {
			return fmt.Errorf("route %q: at least one upstream required", r.Path)
		}
		for j := range r.Upstreams {
			u := &r.Upstreams[j]
			if u.URL == "" {
				return fmt.Errorf("route %q upstream %d: url required", r.Path, j)
			}
			if _, err := url.Parse(u.URL); err != nil {
				return fmt.Errorf("route %q upstream %d: invalid url: %w", r.Path, j, err)
			}
			if u.Weight <= 0 {
				u.Weight = 1
			}
		}
		if r.Limit != "" {
			if _, ok := c.RateLimits[r.Limit]; !ok {
				return fmt.Errorf("route %q: unknown rate-limit profile %q", r.Path, r.Limit)
			}
		}
		if r.Timeout < 0 {
			return fmt.Errorf("route %q: timeout must not be negative", r.Path)
		}
	}
	return nil
}

// UpstreamURLs returns every upstream URL referenced by the config, for
// reconciling the health registry.
func (c *Config) UpstreamURLs() []string {
	var urls []string
	for _, r := range c.Routes {
		for _, u := range r.Upstreams {
			urls = append(urls, u.URL)
		}
	}
	return urls
}
