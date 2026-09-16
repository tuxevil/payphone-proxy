package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/tuxevil/payphone-proxy/internal/payments"
)

type Config struct {
	Addr                string
	DatabasePath        string
	PayPhoneBaseURL     string
	PayPhoneToken       string
	PublicBaseURL       string
	PayPhoneReturnPath  string
	PayPhoneResponseURL string
	Projects            map[string]payments.Project
	Stores              map[string]payments.StoreConfig
}

func Load(lookup func(string) string) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("configuration lookup is nil")
	}
	get := func(key, fallback string) string {
		value := strings.TrimSpace(lookup(key))
		if value == "" {
			return fallback
		}
		return value
	}

	config := Config{
		Addr:                get("HTTP_ADDR", ":8080"),
		DatabasePath:        get("DATABASE_PATH", "./data/payments.db"),
		PayPhoneBaseURL:     get("PAYPHONE_BASE_URL", "https://pay.payphonetodoesposible.com/api"),
		PayPhoneToken:       strings.TrimSpace(lookup("PAYPHONE_TOKEN")),
		PublicBaseURL:       strings.TrimRight(get("PUBLIC_BASE_URL", strings.TrimSpace(lookup("PAYPHONE_WEB_DOMAIN"))), "/"),
		PayPhoneReturnPath:  get("PAYPHONE_RETURN_PATH", "/payphone/return"),
		PayPhoneResponseURL: strings.TrimRight(get("PAYPHONE_RESPONSE_URL", ""), "/"),
	}
	if config.PayPhoneToken == "" {
		return Config{}, errors.New("PAYPHONE_TOKEN is required")
	}
	if config.PublicBaseURL == "" {
		return Config{}, errors.New("PUBLIC_BASE_URL is required")
	}
	if err := validateBaseURL(config.PublicBaseURL, "PUBLIC_BASE_URL"); err != nil {
		return Config{}, err
	}
	if err := validateBaseURL(config.PayPhoneBaseURL, "PAYPHONE_BASE_URL"); err != nil {
		return Config{}, err
	}
	if !strings.HasPrefix(config.PayPhoneReturnPath, "/") || strings.Contains(config.PayPhoneReturnPath, "?") || strings.Contains(config.PayPhoneReturnPath, "#") {
		return Config{}, errors.New("PAYPHONE_RETURN_PATH must be a path without query or fragment")
	}
	if config.PayPhoneResponseURL != "" {
		if err := validateBaseURL(config.PayPhoneResponseURL, "PAYPHONE_RESPONSE_URL"); err != nil {
			return Config{}, err
		}
		responseURL, err := url.Parse(config.PayPhoneResponseURL)
		if err != nil || responseURL.Path == "" || responseURL.Path == "/" {
			return Config{}, errors.New("PAYPHONE_RESPONSE_URL must include a callback path")
		}
	}

	stores, err := parseStores(get("PAYPHONE_STORES_JSON", ""))
	if err != nil {
		return Config{}, fmt.Errorf("PAYPHONE_STORES_JSON: %w", err)
	}
	projects, err := parseProjects(get("PAYMENT_PROJECTS_JSON", ""), stores)
	if err != nil {
		return Config{}, fmt.Errorf("PAYMENT_PROJECTS_JSON: %w", err)
	}
	config.Stores = stores
	config.Projects = projects

	return config, nil
}

func (c Config) PaymentsConfig() payments.Config {
	return payments.Config{
		PublicBaseURL:       c.PublicBaseURL,
		PayPhoneReturnPath:  c.PayPhoneReturnPath,
		PayPhoneResponseURL: c.PayPhoneResponseURL,
		Projects:            c.Projects,
		Stores:              c.Stores,
	}
}

func parseStores(raw string) (map[string]payments.StoreConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("at least one store is required")
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("must be a JSON object of alias to storeId: %w", err)
	}
	if len(values) == 0 {
		return nil, errors.New("at least one store is required")
	}
	stores := make(map[string]payments.StoreConfig, len(values))
	for alias, id := range values {
		alias = strings.TrimSpace(alias)
		id = strings.TrimSpace(id)
		if alias == "" || id == "" {
			return nil, errors.New("store aliases and storeIds must be non-empty")
		}
		stores[alias] = payments.StoreConfig{Alias: alias, ID: id}
	}

	return stores, nil
}

type rawProject struct {
	APIKey    string `json:"api_key"`
	Store     string `json:"store"`
	ReturnURL string `json:"return_url"`
}

func parseProjects(raw string, stores map[string]payments.StoreConfig) (map[string]payments.Project, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("at least one project is required")
	}
	var values map[string]rawProject
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("must be a JSON object of project configuration: %w", err)
	}
	if len(values) == 0 {
		return nil, errors.New("at least one project is required")
	}
	projects := make(map[string]payments.Project, len(values))
	seenAPIKeys := make(map[string]string, len(values))
	for id, value := range values {
		id = strings.TrimSpace(id)
		value.APIKey = strings.TrimSpace(value.APIKey)
		value.Store = strings.TrimSpace(value.Store)
		value.ReturnURL = strings.TrimSpace(value.ReturnURL)
		if id == "" || value.APIKey == "" || value.Store == "" {
			return nil, errors.New("project id, api_key and store must be non-empty")
		}
		if previousID, ok := seenAPIKeys[value.APIKey]; ok {
			return nil, fmt.Errorf("duplicate api_key used by projects %q and %q", previousID, id)
		}
		seenAPIKeys[value.APIKey] = id
		if _, ok := stores[value.Store]; !ok {
			return nil, fmt.Errorf("project %q references unknown store %q", id, value.Store)
		}
		if value.ReturnURL != "" {
			if err := validateBaseURL(value.ReturnURL, "project return_url"); err != nil {
				return nil, fmt.Errorf("project %q: %w", id, err)
			}
		}
		projects[id] = payments.Project{ID: id, APIKey: value.APIKey, StoreAlias: value.Store, ReturnURL: value.ReturnURL}
	}

	return projects, nil
}

func validateBaseURL(raw, name string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an absolute URL without credentials, query or fragment", name)
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLocalHost(parsed.Hostname())) {
		return fmt.Errorf("%s must use HTTPS (HTTP is allowed only for localhost)", name)
	}

	return nil
}

func isLocalHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "[::1]" || host == "::1"
}
