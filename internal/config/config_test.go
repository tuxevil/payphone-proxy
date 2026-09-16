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
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"project-secret-012345678901234567890","store":"store-a","return_url":"https://project.example.test/result"}}`,
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
	if !ok || project.APIKey != "project-secret-012345678901234567890" || project.StoreAlias != "store-a" {
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
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"project-secret-012345678901234567890","store":"missing"}}`,
	}
	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "unknown store") {
		t.Fatalf("Load error = %v, want unknown store", err)
	}
}

func TestLoadRejectsDuplicateProjectAPIKeys(t *testing.T) {
	const secret = "same-secret-012345678901234567890123"
	values := map[string]string{
		"PAYPHONE_TOKEN":        "payphone-secret",
		"PUBLIC_BASE_URL":       "https://proxy.example.test",
		"PAYPHONE_STORES_JSON":  `{"store-a":"store-id-a","store-b":"store-id-b"}`,
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"` + secret + `","store":"store-a"},"project-b":{"api_key":"` + secret + `","store":"store-b"}}`,
	}
	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "duplicate api_key") {
		t.Fatalf("Load error = %v, want duplicate api_key", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Load error contains project API key: %v", err)
	}
}

func TestLoadAllowsResponseURLOnSeparateOrigin(t *testing.T) {
	values := map[string]string{
		"PAYPHONE_TOKEN":        "payphone-secret",
		"PUBLIC_BASE_URL":       "https://registered.example.test",
		"PAYPHONE_RESPONSE_URL": "https://payment.example.test/payphone/response",
		"PAYPHONE_STORES_JSON":  `{"store-a":"store-id-a"}`,
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"project-secret-012345678901234567890","store":"store-a"}}`,
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
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"project-secret-012345678901234567890","store":"store-a"}}`,
	}
	loaded, err := config.Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.PublicBaseURL != "https://registered.example.test" {
		t.Fatalf("PublicBaseURL = %q", loaded.PublicBaseURL)
	}
}

func TestLoadRejectsWeakProjectAPIKey(t *testing.T) {
	values := map[string]string{
		"PAYPHONE_TOKEN":        "payphone-secret",
		"PUBLIC_BASE_URL":       "https://proxy.example.test",
		"PAYPHONE_STORES_JSON":  `{"store-a":"store-id-a"}`,
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"too-short","store":"store-a"}}`,
	}
	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "at least 32 characters") {
		t.Fatalf("Load error = %v, want weak api_key error", err)
	}
}

func TestLoadRejectsTemplateProjectAPIKey(t *testing.T) {
	values := map[string]string{
		"PAYPHONE_TOKEN":        "payphone-secret",
		"PUBLIC_BASE_URL":       "https://proxy.example.test",
		"PAYPHONE_STORES_JSON":  `{"store-a":"store-id-a"}`,
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"replace-with-production-project-key","store":"store-a"}}`,
	}
	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "template placeholder") {
		t.Fatalf("Load error = %v, want placeholder error", err)
	}
}

func TestLoadRejectsTemplatePayPhoneToken(t *testing.T) {
	values := map[string]string{
		"PAYPHONE_TOKEN":        "replace-with-payphone-developer-token",
		"PUBLIC_BASE_URL":       "https://proxy.example.test",
		"PAYPHONE_STORES_JSON":  `{"store-a":"store-id-a"}`,
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"project-secret-012345678901234567890","store":"store-a"}}`,
	}
	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "template placeholder") {
		t.Fatalf("Load error = %v, want placeholder error", err)
	}
}

func TestLoadRejectsAliasesThatNormalizeToTheSameKey(t *testing.T) {
	values := map[string]string{
		"PAYPHONE_TOKEN":        "payphone-secret",
		"PUBLIC_BASE_URL":       "https://proxy.example.test",
		"PAYPHONE_STORES_JSON":  `{" store-a ":"store-id-a","store-a":"store-id-b"}`,
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"project-secret-012345678901234567890","store":"store-a"}}`,
	}
	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "duplicate store alias") {
		t.Fatalf("Load error = %v, want duplicate alias error", err)
	}
}

func TestLoadRejectsCallbackPathThatConflictsWithAPI(t *testing.T) {
	values := map[string]string{
		"PAYPHONE_TOKEN":        "payphone-secret",
		"PUBLIC_BASE_URL":       "https://proxy.example.test",
		"PAYPHONE_RETURN_PATH":  "/checkout/callback",
		"PAYPHONE_STORES_JSON":  `{"store-a":"store-id-a"}`,
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"project-secret-012345678901234567890","store":"store-a"}}`,
	}
	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "reserved API route") {
		t.Fatalf("Load error = %v, want reserved route error", err)
	}
}

func TestLoadRejectsResponseURLThatConflictsWithHealth(t *testing.T) {
	values := map[string]string{
		"PAYPHONE_TOKEN":        "payphone-secret",
		"PUBLIC_BASE_URL":       "https://proxy.example.test",
		"PAYPHONE_RESPONSE_URL": "https://proxy.example.test/healthz",
		"PAYPHONE_STORES_JSON":  `{"store-a":"store-id-a"}`,
		"PAYMENT_PROJECTS_JSON": `{"project-a":{"api_key":"project-secret-012345678901234567890","store":"store-a"}}`,
	}
	_, err := config.Load(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "reserved API route") {
		t.Fatalf("Load error = %v, want reserved route error", err)
	}
}
