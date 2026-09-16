package payments

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/tuxevil/payphone-proxy/internal/payphone"
)

var (
	ErrInvalidInput        = errors.New("invalid payment input")
	ErrInvalidConfirmation = errors.New("PayPhone confirmation does not match payment")
	ErrProvider            = errors.New("PayPhone provider error")
)

type CreateInput struct {
	OrderID   string
	Amount    int64
	Currency  string
	Reference string
}

type Service struct {
	config   Config
	repo     Repository
	provider payphone.Client
	now      func() time.Time
	newToken func(int) (string, error)
}

func NewService(config Config, repo Repository, provider payphone.Client) *Service {
	return &Service{
		config:   config,
		repo:     repo,
		provider: provider,
		now:      time.Now,
		newToken: randomToken,
	}
}

func (s *Service) Authenticate(apiKey string) (Project, bool) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return Project{}, false
	}
	for _, project := range s.config.Projects {
		if len(project.APIKey) == len(apiKey) && subtle.ConstantTimeCompare([]byte(project.APIKey), []byte(apiKey)) == 1 {
			return project, true
		}
	}

	return Project{}, false
}

func (s *Service) Create(ctx context.Context, project Project, idempotencyKey string, input CreateInput) (Payment, bool, error) {
	if err := validateCreateInput(project, idempotencyKey, input); err != nil {
		return Payment{}, false, err
	}

	store, ok := s.config.Stores[project.StoreAlias]
	if !ok || strings.TrimSpace(store.ID) == "" {
		return Payment{}, false, fmt.Errorf("%w: project store is not configured", ErrInvalidInput)
	}

	publicToken, err := s.newToken(32)
	if err != nil {
		return Payment{}, false, fmt.Errorf("generate public token: %w", err)
	}
	clientTransactionID, err := s.newToken(20)
	if err != nil {
		return Payment{}, false, fmt.Errorf("generate client transaction id: %w", err)
	}
	paymentID, err := s.newToken(16)
	if err != nil {
		return Payment{}, false, fmt.Errorf("generate payment id: %w", err)
	}

	now := s.now().UTC()
	checkoutURL := strings.TrimRight(s.config.PublicBaseURL, "/") + "/checkout/" + publicToken
	payment := Payment{
		ID:                  "pay_" + paymentID,
		ProjectID:           project.ID,
		OrderID:             input.OrderID,
		Amount:              input.Amount,
		Currency:            strings.ToUpper(strings.TrimSpace(input.Currency)),
		StoreAlias:          project.StoreAlias,
		StoreID:             store.ID,
		Reference:           input.Reference,
		IdempotencyKey:      idempotencyKey,
		ClientTransactionID: clientTransactionID,
		PublicToken:         publicToken,
		CheckoutURL:         checkoutURL,
		ReturnURL:           project.ReturnURL,
		Status:              StatusPreparing,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	reserved, created, err := s.repo.Reserve(ctx, payment)
	if err != nil {
		return Payment{}, false, err
	}
	if !created {
		if !samePaymentInput(reserved, input, project) {
			return Payment{}, false, ErrIdempotencyConflict
		}
		return reserved, false, nil
	}

	prepared, err := s.provider.Prepare(ctx, payphone.PrepareRequest{
		Amount:              input.Amount,
		AmountWithoutTax:    input.Amount,
		ClientTransactionID: payment.ClientTransactionID,
		Reference:           input.Reference,
		StoreID:             store.ID,
		Currency:            payment.Currency,
		ResponseURL:         s.payPhoneReturnURL(),
		CancellationURL:     checkoutURL,
		TimeZone:            -5,
	})
	if err != nil {
		payment.Status = statusForProviderError(err)
		payment.UpdatedAt = s.now().UTC()
		_ = s.repo.Update(ctx, payment)
		return Payment{}, false, fmt.Errorf("%w: %v", ErrProvider, err)
	}
	if err := validatePreparedResponse(prepared); err != nil {
		payment.Status = StatusFailed
		payment.UpdatedAt = s.now().UTC()
		_ = s.repo.Update(ctx, payment)
		return Payment{}, false, err
	}

	payment.ProviderPaymentID = prepared.PaymentID
	payment.PayWithCard = prepared.PayWithCard
	payment.PayWithPayPhone = prepared.PayWithPayPhone
	payment.Status = StatusPending
	payment.UpdatedAt = s.now().UTC()
	if err := s.repo.Update(ctx, payment); err != nil {
		return Payment{}, false, err
	}

	return payment, true, nil
}

func (s *Service) GetForProject(ctx context.Context, projectID, id string) (Payment, error) {
	payment, err := s.repo.ByID(ctx, id)
	if err != nil {
		return Payment{}, err
	}
	if payment.ProjectID != projectID {
		return Payment{}, ErrNotFound
	}

	return payment, nil
}

func (s *Service) GetByPublicToken(ctx context.Context, token string) (Payment, error) {
	return s.repo.ByPublicToken(ctx, token)
}

func (s *Service) HandleReturn(ctx context.Context, providerID int64, clientTransactionID string) (Payment, error) {
	if providerID <= 0 || strings.TrimSpace(clientTransactionID) == "" {
		return Payment{}, ErrInvalidInput
	}

	payment, err := s.repo.ByClientTransactionID(ctx, clientTransactionID)
	if err != nil {
		return Payment{}, err
	}
	if payment.Status == StatusPaid || payment.Status == StatusCancelled || payment.Status == StatusFailed {
		if payment.ProviderTransactionID != 0 && payment.ProviderTransactionID != providerID {
			return Payment{}, ErrInvalidConfirmation
		}
		return payment, nil
	}

	confirmed, err := s.provider.Confirm(ctx, payphone.ConfirmRequest{
		ID:                  providerID,
		ClientTransactionID: clientTransactionID,
	})
	if err != nil {
		return Payment{}, fmt.Errorf("%w: %v", ErrProvider, err)
	}
	if err := validateConfirmation(payment, confirmed, providerID); err != nil {
		return Payment{}, err
	}

	payment.ProviderTransactionID = confirmed.TransactionID
	if payment.ProviderTransactionID == 0 {
		payment.ProviderTransactionID = providerID
	}
	switch confirmed.StatusCode {
	case 3:
		payment.Status = StatusPaid
	case 2:
		payment.Status = StatusCancelled
	default:
		payment.Status = StatusFailed
	}
	payment.UpdatedAt = s.now().UTC()
	if err := s.repo.Update(ctx, payment); err != nil {
		return Payment{}, err
	}

	return payment, nil
}

func (s *Service) payPhoneReturnURL() string {
	if responseURL := strings.TrimSpace(s.config.PayPhoneResponseURL); responseURL != "" {
		return responseURL
	}

	path := s.config.PayPhoneReturnPath
	if path == "" {
		path = "/payphone/return"
	}
	return strings.TrimRight(s.config.PublicBaseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

// PayPhoneReturnPath returns the callback path handled by the HTTP server.
// Invalid direct constructions fall back to the default path; environment
// configuration validates the complete response URL before constructing the
// service.
func (s *Service) PayPhoneReturnPath() string {
	if responseURL := strings.TrimSpace(s.config.PayPhoneResponseURL); responseURL != "" {
		if parsed, err := url.Parse(responseURL); err == nil && parsed.Path != "" {
			return parsed.Path
		}
	}
	if path := strings.TrimSpace(s.config.PayPhoneReturnPath); path != "" && strings.HasPrefix(path, "/") {
		return path
	}
	return "/payphone/return"
}

func validateCreateInput(project Project, idempotencyKey string, input CreateInput) error {
	if strings.TrimSpace(project.ID) == "" || strings.TrimSpace(project.StoreAlias) == "" {
		return fmt.Errorf("%w: project is incomplete", ErrInvalidInput)
	}
	if strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 128 {
		return fmt.Errorf("%w: Idempotency-Key must contain 1 to 128 characters", ErrInvalidInput)
	}
	if strings.TrimSpace(input.OrderID) == "" || len(input.OrderID) > 200 {
		return fmt.Errorf("%w: order_id must contain 1 to 200 characters", ErrInvalidInput)
	}
	if len(input.Reference) > 100 {
		return fmt.Errorf("%w: reference must contain at most 100 characters", ErrInvalidInput)
	}
	if input.Amount <= 0 {
		return fmt.Errorf("%w: amount must be positive integer cents", ErrInvalidInput)
	}
	if strings.ToUpper(strings.TrimSpace(input.Currency)) != "USD" {
		return fmt.Errorf("%w: only USD is supported in the first version", ErrInvalidInput)
	}

	return nil
}

func samePaymentInput(payment Payment, input CreateInput, project Project) bool {
	return payment.ProjectID == project.ID &&
		payment.OrderID == input.OrderID &&
		payment.Amount == input.Amount &&
		payment.Currency == strings.ToUpper(strings.TrimSpace(input.Currency)) &&
		payment.StoreAlias == project.StoreAlias &&
		payment.Reference == input.Reference
}

func validatePreparedResponse(response payphone.PrepareResponse) error {
	if strings.TrimSpace(response.PaymentID) == "" || strings.TrimSpace(response.PayWithCard) == "" {
		return fmt.Errorf("%w: PayPhone Prepare returned no payment link", ErrProvider)
	}
	for _, rawURL := range []string{response.PayWithCard, response.PayWithPayPhone} {
		if rawURL == "" {
			continue
		}
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "pay.payphonetodoesposible.com" || parsed.User != nil {
			return fmt.Errorf("%w: PayPhone returned an unexpected payment host", ErrProvider)
		}
	}

	return nil
}

func validateConfirmation(payment Payment, confirmed payphone.ConfirmResponse, providerID int64) error {
	if confirmed.ClientTransactionID != payment.ClientTransactionID {
		return ErrInvalidConfirmation
	}
	if confirmed.Amount != payment.Amount || strings.ToUpper(confirmed.Currency) != payment.Currency {
		return ErrInvalidConfirmation
	}
	if confirmed.TransactionID != 0 && confirmed.TransactionID != providerID {
		return ErrInvalidConfirmation
	}

	return nil
}

func statusForProviderError(err error) Status {
	var httpError *payphone.HTTPError
	if errors.As(err, &httpError) {
		return StatusFailed
	}

	return StatusUnknown
}

func randomToken(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("token length must be positive")
	}
	buffer := make([]byte, length)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(buffer)
	return encoded[:length], nil
}
