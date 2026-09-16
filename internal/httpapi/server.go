package httpapi

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/tuxevil/payphone-proxy/internal/payments"
)

type Server struct {
	service            *payments.Service
	payPhoneReturnPath string
}

func New(service *payments.Service) http.Handler {
	server := &Server{service: service, payPhoneReturnPath: service.PayPhoneReturnPath()}
	return server
}

func (s *Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/v1/payments":
		s.createPayment(response, request)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v1/payments/"):
		s.getPayment(response, request)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/checkout/"):
		s.checkout(response, request)
	case request.Method == http.MethodGet && request.URL.Path == s.payPhoneReturnPath:
		s.payPhoneReturn(response, request)
	case request.Method == http.MethodGet && request.URL.Path == "/healthz":
		writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
	default:
		writeError(response, http.StatusNotFound, "not_found", "route not found")
	}
}

type createPaymentRequest struct {
	OrderID   string `json:"order_id"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Reference string `json:"reference"`
}

type paymentResponse struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id,omitempty"`
	OrderID     string `json:"order_id"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
	CheckoutURL string `json:"checkout_url,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func (s *Server) createPayment(response http.ResponseWriter, request *http.Request) {
	project, ok := s.authenticatedProject(request)
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized", "invalid project credentials")
		return
	}
	idempotencyKey := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Idempotency-Key is required")
		return
	}

	requestBody := http.MaxBytesReader(response, request.Body, 64*1024)
	decoder := json.NewDecoder(requestBody)
	decoder.DisallowUnknownFields()
	var input createPaymentRequest
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	payment, created, err := s.service.Create(request.Context(), project, idempotencyKey, payments.CreateInput{
		OrderID:   input.OrderID,
		Amount:    input.Amount,
		Currency:  input.Currency,
		Reference: input.Reference,
	})
	if err != nil {
		s.writeServiceError(response, err)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(response, status, paymentResponseFrom(payment, true))
}

func (s *Server) getPayment(response http.ResponseWriter, request *http.Request) {
	project, ok := s.authenticatedProject(request)
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized", "invalid project credentials")
		return
	}
	id := strings.TrimPrefix(request.URL.Path, "/v1/payments/")
	if id == "" || strings.Contains(id, "/") {
		writeError(response, http.StatusNotFound, "not_found", "payment not found")
		return
	}

	payment, err := s.service.GetForProject(request.Context(), project.ID, id)
	if err != nil {
		if errors.Is(err, payments.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "payment not found")
			return
		}
		writeError(response, http.StatusInternalServerError, "internal_error", "could not load payment")
		return
	}
	writeJSON(response, http.StatusOK, paymentResponseFrom(payment, true))
}

func (s *Server) checkout(response http.ResponseWriter, request *http.Request) {
	token := strings.TrimPrefix(request.URL.Path, "/checkout/")
	if token == "" || strings.Contains(token, "/") {
		writeError(response, http.StatusNotFound, "not_found", "checkout not found")
		return
	}
	payment, err := s.service.GetByPublicToken(request.Context(), token)
	if err != nil {
		writeError(response, http.StatusNotFound, "not_found", "checkout not found")
		return
	}
	if payment.Status != payments.StatusPending {
		writeError(response, http.StatusConflict, "checkout_unavailable", "payment is no longer pending")
		return
	}

	response.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	response.Header().Set("Referrer-Policy", "origin")
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := checkoutRedirectTemplate.Execute(response, struct {
		PayWithCard string
	}{
		PayWithCard: payment.PayWithCard,
	}); err != nil {
		return
	}
}

func (s *Server) payPhoneReturn(response http.ResponseWriter, request *http.Request) {
	providerID, err := strconv.ParseInt(strings.TrimSpace(request.URL.Query().Get("id")), 10, 64)
	if err != nil || providerID <= 0 {
		writeError(response, http.StatusBadRequest, "invalid_return", "PayPhone id is required")
		return
	}
	clientTransactionID := callbackClientTransactionID(request.URL.Query())
	if clientTransactionID == "" {
		writeError(response, http.StatusBadRequest, "invalid_return", "clientTransactionId is required")
		return
	}

	payment, err := s.service.HandleReturn(request.Context(), providerID, clientTransactionID)
	if err != nil {
		s.writeServiceError(response, err)
		return
	}
	if payment.ReturnURL == "" {
		writeJSON(response, http.StatusOK, paymentResponseFrom(payment, false))
		return
	}

	returnURL, err := appendResult(payment.ReturnURL, payment)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "invalid_return_destination", "configured return URL is invalid")
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	http.Redirect(response, request, returnURL, http.StatusSeeOther)
}

func callbackClientTransactionID(query url.Values) string {
	for _, key := range []string{"clientTransactionId", "clientTransactionID", "clientTxId"} {
		if value := strings.TrimSpace(query.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func (s *Server) authenticatedProject(request *http.Request) (payments.Project, bool) {
	value := strings.TrimSpace(request.Header.Get("Authorization"))
	if len(value) < len("Bearer ") || !strings.EqualFold(value[:len("Bearer ")], "Bearer ") {
		return payments.Project{}, false
	}
	return s.service.Authenticate(strings.TrimSpace(value[len("Bearer "):]))
}

func (s *Server) writeServiceError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, payments.ErrInvalidInput):
		writeError(response, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, payments.ErrIdempotencyConflict):
		writeError(response, http.StatusConflict, "idempotency_conflict", err.Error())
	case errors.Is(err, payments.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", "payment not found")
	case errors.Is(err, payments.ErrInvalidConfirmation):
		writeError(response, http.StatusUnprocessableEntity, "confirmation_mismatch", "PayPhone confirmation does not match payment")
	case errors.Is(err, payments.ErrProvider):
		writeError(response, http.StatusBadGateway, "provider_error", "PayPhone could not process the payment")
	default:
		writeError(response, http.StatusInternalServerError, "internal_error", "could not process payment")
	}
}

func paymentResponseFrom(payment payments.Payment, includeProject bool) paymentResponse {
	projectID := ""
	if includeProject {
		projectID = payment.ProjectID
	}
	return paymentResponse{
		ID:          payment.ID,
		ProjectID:   projectID,
		OrderID:     payment.OrderID,
		Amount:      payment.Amount,
		Currency:    payment.Currency,
		Status:      string(payment.Status),
		CheckoutURL: payment.CheckoutURL,
		CreatedAt:   payment.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
		UpdatedAt:   payment.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
	}
}

func appendResult(rawURL string, payment payments.Payment) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("payment_id", payment.ID)
	query.Set("order_id", payment.OrderID)
	query.Set("status", string(payment.Status))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

var checkoutRedirectTemplate = template.Must(template.New("checkout-redirect").Parse(`<!doctype html>
<html lang="es">
<head>
  <meta charset="utf-8">
  <meta name="referrer" content="origin">
  <meta http-equiv="refresh" content="0;url={{.PayWithCard}}">
  <title>Redirigiendo a PayPhone</title>
</head>
<body>
  <p>Redirigiendo a PayPhone…</p>
  <noscript><a href="{{.PayWithCard}}">Continuar a PayPhone</a></noscript>
</body>
</html>`))
