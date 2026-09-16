package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckAcceptsHealthyEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := check(server.Client(), server.URL); err != nil {
		t.Fatalf("check returned error for healthy endpoint: %v", err)
	}
}

func TestCheckRejectsUnhealthyEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	if err := check(server.Client(), server.URL); err == nil {
		t.Fatal("check returned nil for unhealthy endpoint")
	}
}
