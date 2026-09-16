package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tuxevil/payphone-proxy/internal/httpapi"
	"github.com/tuxevil/payphone-proxy/internal/payments"
	"github.com/tuxevil/payphone-proxy/internal/payphone"
)

type prepareCall struct {
	StoreID             string
	ClientTransactionID string
	ResponseURL         string
}

type fakePayPhone struct {
	prepareCalls []prepareCall
	confirmCalls []payphone.ConfirmRequest
	confirm      payphone.ConfirmResponse
	prepareErr   error
	confirmErr   error
}

func (f *fakePayPhone) Prepare(_ context.Context, request payphone.PrepareRequest) (payphone.PrepareResponse, error) {
	f.prepareCalls = append(f.prepareCalls, prepareCall{
		StoreID:             request.StoreID,
		ClientTransactionID: request.ClientTransactionID,
		ResponseURL:         request.ResponseURL,
	})
	if f.prepareErr != nil {
		return payphone.PrepareResponse{}, f.prepareErr
	}

	return payphone.PrepareResponse{
		PaymentID:       "test-payment-id",
		PayWithCard:     "https://pay.payphonetodoesposible.com/Anonymous/Index?paymentId=test-payment-id",
		PayWithPayPhone: "https://pay.payphonetodoesposible.com/PayPhone/Index?paymentId=test-payment-id",
	}, nil
}

func (f *fakePayPhone) Confirm(_ context.Context, request payphone.ConfirmRequest) (payphone.ConfirmResponse, error) {
	f.confirmCalls = append(f.confirmCalls, request)
	if f.confirmErr != nil {
		return payphone.ConfirmResponse{}, f.confirmErr
	}
	return f.confirm, nil
}

func TestCreatePaymentIsIdempotentAndUsesProjectStore(t *testing.T) {
	provider := &fakePayPhone{}
	repository := payments.NewMemoryRepository()
	service := payments.NewService(payments.Config{
		PublicBaseURL: "https://proxy.example.test",
		Projects: map[string]payments.Project{
			"project-a": {
				ID:         "project-a",
				APIKey:     "project-a-secret",
				StoreAlias: "store-a",
			},
		},
		Stores: map[string]payments.StoreConfig{
			"store-a": {Alias: "store-a", ID: "store-id-a"},
		},
	}, repository, provider)
	handler := httpapi.New(service)

	first := createPayment(t, handler, "project-a-secret", "request-1", `{"order_id":"order-1","amount":100,"currency":"USD"}`)
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first create status = %d, want %d; body: %s", first.StatusCode, http.StatusCreated, first.Body)
	}

	second := createPayment(t, handler, "project-a-secret", "request-1", `{"order_id":"order-1","amount":100,"currency":"USD"}`)
	if second.StatusCode != http.StatusOK {
		t.Fatalf("idempotent create status = %d, want %d; body: %s", second.StatusCode, http.StatusOK, second.Body)
	}

	var firstPayload, secondPayload struct {
		ID          string `json:"id"`
		CheckoutURL string `json:"checkout_url"`
		Status      string `json:"status"`
	}
	decodeJSON(t, first.Body, &firstPayload)
	decodeJSON(t, second.Body, &secondPayload)
	if firstPayload.ID == "" || firstPayload.ID != secondPayload.ID {
		t.Fatalf("idempotent response IDs = %q and %q", firstPayload.ID, secondPayload.ID)
	}
	if firstPayload.CheckoutURL == "" || !strings.HasPrefix(firstPayload.CheckoutURL, "https://proxy.example.test/checkout/") {
		t.Fatalf("checkout URL = %q", firstPayload.CheckoutURL)
	}
	if firstPayload.Status != "pending" || secondPayload.Status != "pending" {
		t.Fatalf("statuses = %q and %q, want pending", firstPayload.Status, secondPayload.Status)
	}
	if len(provider.prepareCalls) != 1 {
		t.Fatalf("Prepare calls = %d, want 1", len(provider.prepareCalls))
	}
	if provider.prepareCalls[0].StoreID != "store-id-a" {
		t.Fatalf("Prepare storeId = %q, want store-id-a", provider.prepareCalls[0].StoreID)
	}
	if provider.prepareCalls[0].ClientTransactionID == "" {
		t.Fatal("Prepare clientTransactionId is empty")
	}
	conflicting := createPayment(t, handler, "project-a-secret", "request-1", `{"order_id":"order-1","amount":101,"currency":"USD"}`)
	if conflicting.StatusCode != http.StatusConflict {
		t.Fatalf("conflicting idempotent create status = %d, want %d; body: %s", conflicting.StatusCode, http.StatusConflict, conflicting.Body)
	}
}

func TestCheckoutRedirectsToPayPhoneCardWithOriginReferrer(t *testing.T) {
	provider := &fakePayPhone{}
	repository := payments.NewMemoryRepository()
	service := payments.NewService(payments.Config{
		PublicBaseURL: "https://proxy.example.test",
		Projects: map[string]payments.Project{
			"project-a": {ID: "project-a", APIKey: "project-a-secret", StoreAlias: "store-a"},
		},
		Stores: map[string]payments.StoreConfig{
			"store-a": {Alias: "store-a", ID: "store-id-a"},
		},
	}, repository, provider)
	handler := httpapi.New(service)
	created := createPayment(t, handler, "project-a-secret", "request-checkout", `{"order_id":"order-checkout","amount":100,"currency":"USD"}`)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body: %s", created.StatusCode, http.StatusCreated, created.Body)
	}
	var payload struct {
		CheckoutURL string `json:"checkout_url"`
	}
	decodeJSON(t, created.Body, &payload)

	checkoutRequest := httptest.NewRequest(http.MethodGet, payload.CheckoutURL, nil)
	checkoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(checkoutResponse, checkoutRequest)
	if checkoutResponse.Code != http.StatusOK {
		t.Fatalf("checkout status = %d, want %d; body: %s", checkoutResponse.Code, http.StatusOK, checkoutResponse.Body.String())
	}
	if got := checkoutResponse.Header().Get("Referrer-Policy"); got != "origin" {
		t.Fatalf("Referrer-Policy = %q, want origin", got)
	}
	body := checkoutResponse.Body.String()
	if !strings.Contains(body, "http-equiv=\"refresh\"") {
		t.Fatalf("checkout is not an automatic redirect page: %s", body)
	}
	if !strings.Contains(body, "https://pay.payphonetodoesposible.com/Anonymous/Index?paymentId=test-payment-id") {
		t.Fatalf("checkout does not target PayPhone card URL: %s", body)
	}
	if strings.Contains(body, "PayPhone/Index") {
		t.Fatalf("checkout still exposes the PayPhone wallet option: %s", body)
	}
}

func TestPayPhoneReturnConfirmsAndRedirectsToProject(t *testing.T) {
	provider := &fakePayPhone{
		confirm: payphone.ConfirmResponse{
			ClientTransactionID: "will-be-replaced",
			TransactionID:       12345,
			StatusCode:          3,
			Amount:              100,
			Currency:            "USD",
		},
	}
	repository := payments.NewMemoryRepository()
	service := payments.NewService(payments.Config{
		PublicBaseURL:       "https://proxy.example.test",
		PayPhoneResponseURL: "https://proxy.example.test/payphone/response",
		Projects: map[string]payments.Project{
			"project-a": {
				ID: "project-a", APIKey: "project-a-secret", StoreAlias: "store-a",
				ReturnURL: "https://project.example.test/payment-result",
			},
		},
		Stores: map[string]payments.StoreConfig{
			"store-a": {Alias: "store-a", ID: "store-id-a"},
		},
	}, repository, provider)
	handler := httpapi.New(service)
	created := createPayment(t, handler, "project-a-secret", "request-return", `{"order_id":"order-return","amount":100,"currency":"USD"}`)
	var createdPayload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, created.Body, &createdPayload)
	payment, err := repository.ByID(context.Background(), createdPayload.ID)
	if err != nil {
		t.Fatalf("load payment: %v", err)
	}
	provider.confirm.ClientTransactionID = payment.ClientTransactionID
	if len(provider.prepareCalls) != 1 || provider.prepareCalls[0].ResponseURL != "https://proxy.example.test/payphone/response" {
		t.Fatalf("Prepare response URL = %#v", provider.prepareCalls)
	}

	returnRequest := httptest.NewRequest(http.MethodGet, "/payphone/response?id=12345&clientTransactionId="+payment.ClientTransactionID, nil)
	returnResponse := httptest.NewRecorder()
	handler.ServeHTTP(returnResponse, returnRequest)
	if returnResponse.Code != http.StatusSeeOther {
		t.Fatalf("return status = %d, want %d; body: %s", returnResponse.Code, http.StatusSeeOther, returnResponse.Body.String())
	}
	location := returnResponse.Header().Get("Location")
	if !strings.HasPrefix(location, "https://project.example.test/payment-result?") || !strings.Contains(location, "status=paid") {
		t.Fatalf("return Location = %q", location)
	}
	if len(provider.confirmCalls) != 1 || provider.confirmCalls[0].ID != 12345 || provider.confirmCalls[0].ClientTransactionID != payment.ClientTransactionID {
		t.Fatalf("Confirm calls = %#v", provider.confirmCalls)
	}
	settled, err := repository.ByID(context.Background(), payment.ID)
	if err != nil {
		t.Fatalf("load settled payment: %v", err)
	}
	if settled.Status != payments.StatusPaid {
		t.Fatalf("settled status = %q, want paid", settled.Status)
	}

	replayedRequest := httptest.NewRequest(http.MethodGet, "/payphone/response?id=99999&clientTransactionId="+payment.ClientTransactionID, nil)
	replayedResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayedResponse, replayedRequest)
	if replayedResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("replayed return status = %d, want %d; body: %s", replayedResponse.Code, http.StatusUnprocessableEntity, replayedResponse.Body.String())
	}
}

func TestProjectCannotReadAnotherProjectPayment(t *testing.T) {
	provider := &fakePayPhone{}
	repository := payments.NewMemoryRepository()
	service := payments.NewService(payments.Config{
		PublicBaseURL: "https://proxy.example.test",
		Projects: map[string]payments.Project{
			"project-a": {ID: "project-a", APIKey: "project-a-secret", StoreAlias: "store-a"},
			"project-b": {ID: "project-b", APIKey: "project-b-secret", StoreAlias: "store-b"},
		},
		Stores: map[string]payments.StoreConfig{
			"store-a": {Alias: "store-a", ID: "store-id-a"},
			"store-b": {Alias: "store-b", ID: "store-id-b"},
		},
	}, repository, provider)
	handler := httpapi.New(service)
	created := createPayment(t, handler, "project-a-secret", "request-isolation", `{"order_id":"order-isolation","amount":100,"currency":"USD"}`)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body: %s", created.StatusCode, http.StatusCreated, created.Body)
	}
	var payload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, created.Body, &payload)

	getRequest := httptest.NewRequest(http.MethodGet, "/v1/payments/"+payload.ID, nil)
	getRequest.Header.Set("Authorization", "Bearer project-b-secret")
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusNotFound {
		t.Fatalf("cross-project GET status = %d, want %d; body: %s", getResponse.Code, http.StatusNotFound, getResponse.Body.String())
	}
}

func TestProviderFailureIsPersistedAndReturnedAsBadGateway(t *testing.T) {
	provider := &fakePayPhone{prepareErr: &payphone.HTTPError{StatusCode: http.StatusBadGateway, Body: "temporary provider failure"}}
	repository := payments.NewMemoryRepository()
	service := payments.NewService(payments.Config{
		PublicBaseURL: "https://proxy.example.test",
		Projects: map[string]payments.Project{
			"project-a": {ID: "project-a", APIKey: "project-a-secret", StoreAlias: "store-a"},
		},
		Stores: map[string]payments.StoreConfig{
			"store-a": {Alias: "store-a", ID: "store-id-a"},
		},
	}, repository, provider)
	handler := httpapi.New(service)

	failed := createPayment(t, handler, "project-a-secret", "request-provider-error", `{"order_id":"order-provider-error","amount":100,"currency":"USD"}`)
	if failed.StatusCode != http.StatusBadGateway {
		t.Fatalf("provider error status = %d, want %d; body: %s", failed.StatusCode, http.StatusBadGateway, failed.Body)
	}
	if !strings.Contains(failed.Body, `"provider_error"`) {
		t.Fatalf("provider error body = %s", failed.Body)
	}
	if len(provider.prepareCalls) != 1 {
		t.Fatalf("Prepare calls = %d, want 1", len(provider.prepareCalls))
	}
	payment, err := repository.ByClientTransactionID(context.Background(), provider.prepareCalls[0].ClientTransactionID)
	if err != nil {
		t.Fatalf("load failed payment: %v", err)
	}
	if payment.Status != payments.StatusUnknown {
		t.Fatalf("failed payment status = %q, want unknown", payment.Status)
	}
}

func TestHealthEndpointDoesNotRequireProjectCredentials(t *testing.T) {
	service := payments.NewService(payments.Config{}, payments.NewMemoryRepository(), &fakePayPhone{})
	handler := httpapi.New(service)
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("health body = %q", response.Body.String())
	}
}

func TestCreateRejectsTrailingJSONAndSetsSecurityHeaders(t *testing.T) {
	service := payments.NewService(payments.Config{
		PublicBaseURL: "https://proxy.example.test",
		Projects: map[string]payments.Project{
			"project-a": {ID: "project-a", APIKey: "project-a-secret", StoreAlias: "store-a"},
		},
		Stores: map[string]payments.StoreConfig{
			"store-a": {Alias: "store-a", ID: "store-id-a"},
		},
	}, payments.NewMemoryRepository(), &fakePayPhone{})
	handler := httpapi.New(service)
	request := httptest.NewRequest(http.MethodPost, "/v1/payments", strings.NewReader(`{"order_id":"order","amount":100,"currency":"USD"}{"amount":101}`))
	request.Header.Set("Authorization", "Bearer project-a-secret")
	request.Header.Set("Idempotency-Key", "strict-json")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON status = %d, want 400; body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("X-Frame-Options") != "DENY" || response.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("security headers = %#v", response.Header())
	}
}

func TestCreateRejectsDuplicateJSONKeys(t *testing.T) {
	service := payments.NewService(payments.Config{
		PublicBaseURL: "https://proxy.example.test",
		Projects: map[string]payments.Project{
			"project-a": {ID: "project-a", APIKey: "project-a-secret", StoreAlias: "store-a"},
		},
		Stores: map[string]payments.StoreConfig{
			"store-a": {Alias: "store-a", ID: "store-id-a"},
		},
	}, payments.NewMemoryRepository(), &fakePayPhone{})
	handler := httpapi.New(service)
	request := httptest.NewRequest(http.MethodPost, "/v1/payments", strings.NewReader(`{"order_id":"order","amount":100,"amount":101,"currency":"USD"}`))
	request.Header.Set("Authorization", "Bearer project-a-secret")
	request.Header.Set("Idempotency-Key", "duplicate-json")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate JSON status = %d, want 400; body=%s", response.Code, response.Body.String())
	}
}

func TestCreateRejectsCaseVariantJSONKeys(t *testing.T) {
	service := payments.NewService(payments.Config{
		PublicBaseURL: "https://proxy.example.test",
		Projects: map[string]payments.Project{
			"project-a": {ID: "project-a", APIKey: "project-a-secret", StoreAlias: "store-a"},
		},
		Stores: map[string]payments.StoreConfig{
			"store-a": {Alias: "store-a", ID: "store-id-a"},
		},
	}, payments.NewMemoryRepository(), &fakePayPhone{})
	handler := httpapi.New(service)
	request := httptest.NewRequest(http.MethodPost, "/v1/payments", strings.NewReader(`{"order_id":"order","Amount":100,"amount":101,"currency":"USD"}`))
	request.Header.Set("Authorization", "Bearer project-a-secret")
	request.Header.Set("Idempotency-Key", "case-variant-json")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("case-variant JSON status = %d, want 400; body=%s", response.Code, response.Body.String())
	}
}

func TestReturnAttemptsAreRateLimitedPerTransaction(t *testing.T) {
	provider := &fakePayPhone{confirmErr: errors.New("temporary provider failure")}
	repository := payments.NewMemoryRepository()
	service := payments.NewService(payments.Config{
		PublicBaseURL: "https://proxy.example.test",
		Projects: map[string]payments.Project{
			"project-a": {ID: "project-a", APIKey: "project-a-secret", StoreAlias: "store-a"},
		},
		Stores: map[string]payments.StoreConfig{
			"store-a": {Alias: "store-a", ID: "store-id-a"},
		},
	}, repository, provider)
	handler := httpapi.New(service)
	created := createPayment(t, handler, "project-a-secret", "rate-limit", `{"order_id":"rate-limit","amount":100,"currency":"USD"}`)
	var payload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, created.Body, &payload)
	payment, err := repository.ByID(context.Background(), payload.ID)
	if err != nil {
		t.Fatalf("load payment: %v", err)
	}
	for attempt := 1; attempt <= 7; attempt++ {
		request := httptest.NewRequest(http.MethodGet, "/payphone/return?id=41&clientTransactionId="+payment.ClientTransactionID, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusBadGateway
		if attempt == 7 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("attempt %d status = %d, want %d; body=%s", attempt, response.Code, want, response.Body.String())
		}
		if attempt == 7 && response.Header().Get("Retry-After") != "60" {
			t.Fatalf("Retry-After = %q", response.Header().Get("Retry-After"))
		}
	}
	if got := len(provider.confirmCalls); got != 6 {
		t.Fatalf("Confirm calls = %d, want 6", got)
	}
}

type response struct {
	StatusCode int
	Body       string
}

func createPayment(t *testing.T, handler http.Handler, apiKey, idempotencyKey, body string) response {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/payments", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Idempotency-Key", idempotencyKey)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return response{StatusCode: recorder.Code, Body: recorder.Body.String()}
}

func decodeJSON(t *testing.T, body string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), target); err != nil {
		t.Fatalf("decode response %q: %v", body, err)
	}
}
