// Package payphone contiene el cliente mínimo para el flujo Button de PayPhone.
package payphone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultBaseURL = "https://pay.payphonetodoesposible.com/api"

// Client es la frontera que permite probar el servicio sin llamar a PayPhone.
type Client interface {
	Prepare(context.Context, PrepareRequest) (PrepareResponse, error)
	Confirm(context.Context, ConfirmRequest) (ConfirmResponse, error)
}

// PrepareRequest representa los campos usados por Button/Prepare.
type PrepareRequest struct {
	Amount              int64  `json:"amount"`
	AmountWithoutTax    int64  `json:"amountWithoutTax"`
	AmountWithTax       int64  `json:"amountWithTax"`
	Tax                 int64  `json:"tax"`
	Service             int64  `json:"service"`
	Tip                 int64  `json:"tip"`
	ClientTransactionID string `json:"clientTransactionId"`
	Reference           string `json:"reference,omitempty"`
	StoreID             string `json:"storeId"`
	Currency            string `json:"currency"`
	ResponseURL         string `json:"responseUrl"`
	CancellationURL     string `json:"cancellationUrl,omitempty"`
	TimeZone            int    `json:"timeZone,omitempty"`
}

// PrepareResponse contiene los enlaces que PayPhone entrega al preparar una sesión.
type PrepareResponse struct {
	PaymentID       string `json:"paymentId"`
	PayWithCard     string `json:"payWithCard"`
	PayWithPayPhone string `json:"payWithPayPhone"`
}

// ConfirmRequest representa Button/V2/Confirm.
type ConfirmRequest struct {
	ID                  int64  `json:"id"`
	ClientTransactionID string `json:"clientTxId"`
}

// ConfirmResponse contiene los datos que el proxy valida antes de marcar un pago.
type ConfirmResponse struct {
	ClientTransactionID string `json:"clientTransactionId"`
	TransactionID       int64  `json:"transactionId"`
	StatusCode          int    `json:"statusCode"`
	Amount              int64  `json:"amount"`
	Currency            string `json:"currency"`
	StoreID             string `json:"storeId"`
}

// HTTPError conserva el estado y un cuerpo acotado sin incluir cabeceras secretas.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("payphone returned HTTP %d", e.StatusCode)
	}

	return fmt.Sprintf("payphone returned HTTP %d: %s", e.StatusCode, e.Body)
}

// HTTPClient implementa el cliente REST oficial usando net/http.
type HTTPClient struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func NewHTTPClient(baseURL, token string, client *http.Client) *HTTPClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}
	if client == nil {
		client = http.DefaultClient
	}

	return &HTTPClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Token:      strings.TrimSpace(token),
		HTTPClient: client,
	}
}

func (c *HTTPClient) Prepare(ctx context.Context, request PrepareRequest) (PrepareResponse, error) {
	var response PrepareResponse
	if err := c.doJSON(ctx, http.MethodPost, "/button/Prepare", request, &response); err != nil {
		return PrepareResponse{}, err
	}

	return response, nil
}

func (c *HTTPClient) Confirm(ctx context.Context, request ConfirmRequest) (ConfirmResponse, error) {
	var response ConfirmResponse
	if err := c.doJSON(ctx, http.MethodPost, "/button/V2/Confirm", request, &response); err != nil {
		return ConfirmResponse{}, err
	}

	return response, nil
}

func (c *HTTPClient) doJSON(ctx context.Context, method, path string, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal PayPhone request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create PayPhone request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.Token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	// Treat redirects as terminal responses; the configured endpoint is
	// authoritative.
	httpClient := *c.HTTPClient
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call PayPhone: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("read PayPhone response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return &HTTPError{StatusCode: response.StatusCode, Body: strings.TrimSpace(string(responseBody))}
	}

	if result == nil || len(bytes.TrimSpace(responseBody)) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, result); err != nil {
		return fmt.Errorf("decode PayPhone response: %w", err)
	}

	return nil
}

var _ Client = (*HTTPClient)(nil)
var _ error = (*HTTPError)(nil)
