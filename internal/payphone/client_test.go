package payphone_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tuxevil/payphone-proxy/internal/payphone"
)

func TestHTTPClientUsesButtonEndpointsAndBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", got)
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}

		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch request.URL.Path {
		case "/api/button/Prepare":
			if body["storeId"] != "store-id" || body["clientTransactionId"] != "client-id" {
				t.Errorf("Prepare body = %#v", body)
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"paymentId":"payment-id","payWithCard":"https://pay.payphonetodoesposible.com/Anonymous/Index?paymentId=payment-id"}`))
		case "/api/button/V2/Confirm":
			if body["id"] != float64(123) || body["clientTxId"] != "client-id" {
				t.Errorf("Confirm body = %#v", body)
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"clientTransactionId":"client-id","transactionId":123,"statusCode":3,"amount":100,"currency":"USD"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client := payphone.NewHTTPClient(server.URL+"/api", "test-token", server.Client())
	prepared, err := client.Prepare(context.Background(), payphone.PrepareRequest{
		Amount:              100,
		AmountWithoutTax:    100,
		ClientTransactionID: "client-id",
		StoreID:             "store-id",
		Currency:            "USD",
		ResponseURL:         "https://proxy.example.test/payphone/return",
	})
	if err != nil || prepared.PaymentID != "payment-id" {
		t.Fatalf("Prepare = %#v, err=%v", prepared, err)
	}
	confirmed, err := client.Confirm(context.Background(), payphone.ConfirmRequest{ID: 123, ClientTransactionID: "client-id"})
	if err != nil || confirmed.StatusCode != 3 || confirmed.TransactionID != 123 {
		t.Fatalf("Confirm = %#v, err=%v", confirmed, err)
	}
}

func TestHTTPClientReturnsBoundedProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Error(response, "provider failure", http.StatusBadGateway)
	}))
	defer server.Close()

	client := payphone.NewHTTPClient(server.URL, "secret-token", server.Client())
	_, err := client.Prepare(context.Background(), payphone.PrepareRequest{Amount: 100, StoreID: "store", Currency: "USD", ClientTransactionID: "client", ResponseURL: "https://proxy.example.test/return"})
	if err == nil {
		t.Fatal("Prepare error = nil, want provider error")
	}
	if !strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("Prepare error = %q", err.Error())
	}
}
