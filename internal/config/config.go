package config

import (
	"fmt"
	"net"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen      ListenConfig       `yaml:"listen"`
	HTTP        HTTPConfig         `yaml:"http"`
	Destinations []DestinationConfig `yaml:"destinations"`
	Enrichment  EnrichmentConfig   `yaml:"enrichment"`
	Logging     LoggingConfig      `yaml:"logging"`
	Security    SecurityConfig     `yaml:"security"`
	Telegram    TelegramConfig     `yaml:"telegram"`

	mu sync.RWMutex
}

type ListenConfig struct {
	Address string `yaml:"address"`
	Port    int    `yaml:"port"`
}

type HTTPConfig struct {
	Enabled bool   `yaml:"enabled"`
	Address string `yaml:"address"`
	Port    int    `yaml:"port"`
}

type DestinationConfig struct {
	Name     string `yaml:"name"`
	Address  string `yaml:"address"`
	Port     int    `yaml:"port"`
	Enabled  bool   `yaml:"enabled"`
	Primary  bool   `yaml:"primary"`  // For failover
	Failover string `yaml:"failover"` // Name of failover destination
}

type EnrichmentConfig struct {
	Rules []EnrichmentRule `yaml:"rules"`
}

type EnrichmentRule struct {
	Name      string `yaml:"name"`
	Network   string `yaml:"network"`
	MatchAS   uint32 `yaml:"match_as"`
	SetAS     uint32 `yaml:"set_as"`
	Overwrite bool   `yaml:"overwrite"` // Force overwrite even if AS != match_as
	// Parsed network
	IPNet *net.IPNet `yaml:"-"`
}

type LoggingConfig struct {
	Level         string `yaml:"level"`
	Format        string `yaml:"format"` // "text" or "json"
	StatsInterval int    `yaml:"stats_interval"`
}

type SecurityConfig struct {
	WhitelistEnabled bool     `yaml:"whitelist_enabled"`
	WhitelistSources []string `yaml:"whitelist_sources"`
	// Parsed networks
	WhitelistNets []*net.IPNet `yaml:"-"`
}

type TelegramConfig struct {
	Enabled           bool     `yaml:"enabled"`
	BotToken          string   `yaml:"bot_token"`
	ChatID            string   `yaml:"chat_id"`
	AlertOn           []string `yaml:"alert_on"`            // "startup", "shutdown", "destination_down", "destination_up", "high_drop_rate"
	DropRateThreshold float64  `yaml:"drop_rate_threshold"` // percentage, default 5.0
	HTTPTimeout       int      `yaml:"http_timeout"`        // seconds, default 15
	FlapCooldown      int      `yaml:"flap_cooldown"`       // seconds between alerts for same destination, default 300
	IPv6Fallback      bool     `yaml:"ipv6_fallback"`       // try IPv6 first, fallback to IPv4
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := cfg.parse(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) parse() error {
	// Parse enrichment networks
	for i := range c.Enrichment.Rules {
		_, ipnet, err := net.ParseCIDR(c.Enrichment.Rules[i].Network)
		if err != nil {
			return fmt.Errorf("invalid network %s: %w", c.Enrichment.Rules[i].Network, err)
		}
		c.Enrichment.Rules[i].IPNet = ipnet
	}

	// Parse whitelist networks
	for _, src := range c.Security.WhitelistSources {
		_, ipnet, err := net.ParseCIDR(src)
		if err != nil {
			// Try as single IP
			ip := net.ParseIP(src)
			if ip == nil {
				return fmt.Errorf("invalid whitelist source %s", src)
			}
			if ip.To4() != nil {
				_, ipnet, _ = net.ParseCIDR(src + "/32")
			} else {
				_, ipnet, _ = net.ParseCIDR(src + "/128")
			}
		}
		c.Security.WhitelistNets = append(c.Security.WhitelistNets, ipnet)
	}

	// Set defaults
	if c.Logging.Format == "" {
		c.Logging.Format = "text"
	}
	if c.Logging.StatsInterval == 0 {
		c.Logging.StatsInterval = 60
	}
	if c.HTTP.Address == "" {
		c.HTTP.Address = "127.0.0.1"
	}
	if c.HTTP.Port == 0 {
		c.HTTP.Port = 8080
	}
	if c.Telegram.DropRateThreshold == 0 {
		c.Telegram.DropRateThreshold = 5.0
	}
	if c.Telegram.HTTPTimeout == 0 {
		c.Telegram.HTTPTimeout = 15
	}
	if c.Telegram.FlapCooldown == 0 {
		c.Telegram.FlapCooldown = 300
	}

	return nil
}

func (c *Config) Reload(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var newCfg Config
	if err := yaml.Unmarshal(data, &newCfg); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := newCfg.parse(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Update reloadable fields
	c.Enrichment = newCfg.Enrichment
	c.Security = newCfg.Security
	c.Telegram = newCfg.Telegram
	c.Logging.Level = newCfg.Logging.Level

	return nil
}

func (c *Config) GetEnrichmentRules() []EnrichmentRule {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rules := make([]EnrichmentRule, len(c.Enrichment.Rules))
	copy(rules, c.Enrichment.Rules)
	return rules
}

func (c *Config) IsWhitelisted(ip net.IP) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.Security.WhitelistEnabled {
		return true
	}

	for _, ipnet := range c.Security.WhitelistNets {
		if ipnet.Contains(ip) {
			return true
		}
	}
	return false
}

func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Listen.Address, c.Listen.Port)
}

func (c *Config) HTTPAddr() string {
	return fmt.Sprintf("%s:%d", c.HTTP.Address, c.HTTP.Port)
}
