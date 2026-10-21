package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	envConfigPath             = "STRIPE_EMULATE_CONFIG"
	envAddr                   = "STRIPE_EMULATE_ADDR"
	envDatabaseURL            = "STRIPE_EMULATE_DATABASE_URL"
	envStaticDir              = "STRIPE_EMULATE_STATIC_DIR"
	envPublicBasePath         = "PUBLIC_BASE_PATH"
	envEmulatorPublicBasePath = "STRIPE_EMULATE_PUBLIC_BASE_PATH"
	envPublicBaseURL          = "STRIPE_EMULATE_PUBLIC_BASE_URL"
	envEnvironment            = "STRIPE_EMULATE_ENV"
	envRelayMode              = "STRIPE_EMULATE_RELAY_MODE"
	envRawPayloadStorage      = "STRIPE_EMULATE_RAW_PAYLOAD_STORAGE"
	envRetentionDays          = "STRIPE_EMULATE_RETENTION_DAYS"
	envWebhookAPIVersion      = "STRIPE_EMULATE_WEBHOOK_API_VERSION"
	envRequestLog             = "STRIPE_EMULATE_REQUEST_LOG"

	RawPayloadStore        = "store"
	RawPayloadMetadataOnly = "metadata_only"

	DefaultWebhookAPIVersion = "2025-12-15.clover"
)

type LookupFunc func(string) (string, bool)

type Config struct {
	Addr              string `json:"addr"`
	DatabaseURL       string `json:"database_url"`
	StaticDir         string `json:"static_dir"`
	PublicBasePath    string `json:"public_base_path"`
	PublicBaseURL     string `json:"public_base_url"`
	Environment       string `json:"environment"`
	RelayMode         bool   `json:"relay_mode"`
	RawPayloadStorage string `json:"raw_payload_storage"`
	RetentionDays     int    `json:"retention_days"`
	WebhookAPIVersion string `json:"webhook_api_version"`
	// RequestLogDisabled turns off the per-request log line (on by default).
	RequestLogDisabled bool `json:"request_log_disabled"`
}

func Default() Config {
	return Config{
		Addr:              ":8080",
		DatabaseURL:       ".emulator/emulator.db",
		StaticDir:         "dist/app",
		Environment:       "development",
		RelayMode:         false,
		RawPayloadStorage: RawPayloadStore,
		RetentionDays:     30,
		WebhookAPIVersion: DefaultWebhookAPIVersion,
	}
}

func Load(path string) (Config, error) {
	return LoadWithLookup(path, os.LookupEnv)
}

func LoadWithLookup(path string, lookup LookupFunc) (Config, error) {
	cfg := Default()
	if lookup == nil {
		lookup = os.LookupEnv
	}

	if path == "" {
		if value, ok := lookup(envConfigPath); ok {
			path = value
		}
	}

	if path != "" {
		fileCfg, err := loadFile(path)
		if err != nil {
			return Config{}, err
		}
		cfg = merge(cfg, fileCfg)
	}

	if value, ok := lookup(envAddr); ok {
		cfg.Addr = value
	}
	if value, ok := lookup(envDatabaseURL); ok {
		cfg.DatabaseURL = value
	}
	if value, ok := lookup(envStaticDir); ok {
		cfg.StaticDir = value
	}
	if value, ok := lookup(envPublicBasePath); ok {
		cfg.PublicBasePath = value
	}
	if value, ok := lookup(envEmulatorPublicBasePath); ok {
		cfg.PublicBasePath = value
	}
	if value, ok := lookup(envPublicBaseURL); ok {
		cfg.PublicBaseURL = value
	}
	if value, ok := lookup(envEnvironment); ok {
		cfg.Environment = value
	}
	if value, ok := lookup(envRelayMode); ok {
		cfg.RelayMode = parseBool(value)
	}
	if value, ok := lookup(envRawPayloadStorage); ok {
		cfg.RawPayloadStorage = value
	}
	if value, ok := lookup(envRetentionDays); ok {
		days, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", envRetentionDays, err)
		}
		cfg.RetentionDays = days
	}
	if value, ok := lookup(envWebhookAPIVersion); ok {
		cfg.WebhookAPIVersion = value
	}
	if value, ok := lookup(envRequestLog); ok {
		cfg.RequestLogDisabled = !parseBool(value)
	}
	if cfg.RelayMode {
		cfg.RawPayloadStorage = RawPayloadMetadataOnly
	}
	publicBasePath, err := ValidatePublicBasePath(cfg.PublicBasePath)
	if err != nil {
		return Config{}, err
	}
	cfg.PublicBasePath = publicBasePath

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Addr == "" {
		return errors.New("addr is required")
	}
	if c.DatabaseURL == "" {
		return errors.New("database_url is required")
	}
	if c.StaticDir == "" {
		return errors.New("static_dir is required")
	}
	if _, err := ValidatePublicBasePath(c.PublicBasePath); err != nil {
		return err
	}
	if c.Environment == "" {
		return errors.New("environment is required")
	}
	switch c.RawPayloadStorage {
	case RawPayloadStore, RawPayloadMetadataOnly:
	default:
		return fmt.Errorf("raw_payload_storage must be %q or %q", RawPayloadStore, RawPayloadMetadataOnly)
	}
	if c.RetentionDays < 0 {
		return errors.New("retention_days must be zero or greater")
	}
	if c.WebhookAPIVersion == "" {
		return errors.New("webhook_api_version is required")
	}
	return nil
}

func loadFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config file %q: %w", path, err)
	}
	defer f.Close()

	var cfg Config
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config file %q: %w", path, err)
	}
	return cfg, nil
}

func merge(base Config, override Config) Config {
	if override.Addr != "" {
		base.Addr = override.Addr
	}
	if override.DatabaseURL != "" {
		base.DatabaseURL = override.DatabaseURL
	}
	if override.StaticDir != "" {
		base.StaticDir = override.StaticDir
	}
	if override.PublicBasePath != "" {
		base.PublicBasePath = override.PublicBasePath
	}
	if override.PublicBaseURL != "" {
		base.PublicBaseURL = override.PublicBaseURL
	}
	if override.Environment != "" {
		base.Environment = override.Environment
	}
	if override.RelayMode {
		base.RelayMode = true
	}
	if override.RawPayloadStorage != "" {
		base.RawPayloadStorage = override.RawPayloadStorage
	}
	if override.RetentionDays != 0 {
		base.RetentionDays = override.RetentionDays
	}
	if override.WebhookAPIVersion != "" {
		base.WebhookAPIVersion = override.WebhookAPIVersion
	}
	if override.RequestLogDisabled {
		base.RequestLogDisabled = true
	}
	return base
}

func NormalizePublicBasePath(value string) string {
	normalized, _ := ValidatePublicBasePath(value)
	return normalized
}

func ValidatePublicBasePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return "", nil
	}
	if strings.Contains(value, "://") || strings.ContainsAny(value, "?#") {
		return "", errors.New("public_base_path must be a URL path, not a full URL")
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	value = strings.TrimRight(value, "/")
	for _, part := range strings.Split(value, "/") {
		if part == ".." || part == "." {
			return "", errors.New("public_base_path cannot contain dot path segments")
		}
	}
	if strings.Contains(value, "//") {
		return "", errors.New("public_base_path cannot contain empty path segments")
	}
	return value, nil
}

// ValidatePublicBaseURL normalizes an absolute public base URL. The value must
// be http(s) with a host, may carry a path prefix, and cannot include
// credentials, a query, or a fragment. An empty value stays empty.
func ValidatePublicBaseURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("public_base_url must be a valid URL: %v", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("public_base_url must be an absolute http(s) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("public_base_url cannot contain credentials, query, or fragment")
	}
	return strings.TrimRight(value, "/"), nil
}

// PublicBaseURLWithPath combines a public base URL with an optional base path,
// avoiding a duplicated suffix when the URL already ends with the path.
func PublicBaseURLWithPath(baseURL string, basePath string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	basePath = NormalizePublicBasePath(basePath)
	if baseURL == "" || basePath == "" || strings.HasSuffix(baseURL, basePath) {
		return baseURL
	}
	return baseURL + basePath
}

func parseBool(value string) bool {
	switch value {
	case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON":
		return true
	default:
		return false
	}
}
