package config_test

import (
	"strings"
	"testing"

	"github.com/tuxevil/payphone-proxy/internal/config"
)

func TestLoadParsesProjectsStoresAndDefaults(t *testing.T) {
	values := map[string]string{
		"PAYPHONE_TOKEN":        "payphone-secret",
		"PUBLIC_BASE_URL":       "https://proxy.example.test",
		"PAYPHONE_RESPONSE_URL": "https://proxy.example.test/payphone/response",
		"PAYPHONE_STORES_JSON":  `{"store-a":"store-id-a"}`,
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"project-secret","store":"store-a","return_url":"https://project.example.test/result"}}`,
	}
	loaded, err := config.Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.Addr != ":8080" || loaded.DatabasePath != "./data/payments.db" {
		t.Fatalf("defaults = addr %q database %q", loaded.Addr, loaded.DatabasePath)
	}
	if loaded.PayPhoneToken != "payphone-secret" || loaded.PublicBaseURL != "https://proxy.example.test" {
		t.Fatalf("basic config = %#v", loaded)
	}
	if loaded.PayPhoneResponseURL != "https://proxy.example.test/payphone/response" {
		t.Fatalf("PayPhoneResponseURL = %q", loaded.PayPhoneResponseURL)
	}
	project, ok := loaded.Projects["project-a"]
	if !ok || project.APIKey != "project-secret" || project.StoreAlias != "store-a" {
		t.Fatalf("project = %#v", project)
	}
	store, ok := loaded.Stores["store-a"]
	if !ok || store.ID != "store-id-a" {
		t.Fatalf("store = %#v", store)
	}
}

func TestLoadRejectsProjectWithUnknownStore(t *testing.T) {
	values := map[string]string{
		"PAYPHONE_TOKEN":        "payphone-secret",
		"PUBLIC_BASE_URL":       "https://proxy.example.test",
		"PAYPHONE_STORES_JSON":  `{"store-a":"store-id-a"}`,
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"project-secret","store":"missing"}}`,
	}
	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "unknown store") {
		t.Fatalf("Load error = %v, want unknown store", err)
	}
}

func TestLoadRejectsDuplicateProjectAPIKeys(t *testing.T) {
	values := map[string]string{
		"PAYPHONE_TOKEN":        "payphone-secret",
		"PUBLIC_BASE_URL":       "https://proxy.example.test",
		"PAYPHONE_STORES_JSON":  `{"store-a":"store-id-a","store-b":"store-id-b"}`,
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"same-secret","store":"store-a"},"project-b":{"api_key":"same-secret","store":"store-b"}}`,
	}
	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "duplicate api_key") {
		t.Fatalf("Load error = %v, want duplicate api_key", err)
	}
}

func TestLoadAllowsResponseURLOnSeparateOrigin(t *testing.T) {
	values := map[string]string{
		"PAYPHONE_TOKEN":        "payphone-secret",
		"PUBLIC_BASE_URL":       "https://registered.example.test",
		"PAYPHONE_RESPONSE_URL": "https://payment.example.test/payphone/response",
		"PAYPHONE_STORES_JSON":  `{"store-a":"store-id-a"}`,
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"project-secret","store":"store-a"}}`,
	}
	loaded, err := config.Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.PayPhoneResponseURL != "https://payment.example.test/payphone/response" {
		t.Fatalf("PayPhoneResponseURL = %q", loaded.PayPhoneResponseURL)
	}
}

func TestLoadUsesPayPhoneWebDomainAsPublicBaseURLFallback(t *testing.T) {
	values := map[string]string{
		"PAYPHONE_TOKEN":        "payphone-secret",
		"PAYPHONE_WEB_DOMAIN":   "https://registered.example.test",
		"PAYPHONE_RESPONSE_URL": "https://payment.example.test/payphone/response",
		"PAYPHONE_STORES_JSON":  `{"store-a":"store-id-a"}`,
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"project-secret","store":"store-a"}}`,
	}
	loaded, err := config.Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.PublicBaseURL != "https://registered.example.test" {
		t.Fatalf("PublicBaseURL = %q", loaded.PublicBaseURL)
	}
}
