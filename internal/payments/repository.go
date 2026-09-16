package payments

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrNotFound            = errors.New("payment not found")
	ErrIdempotencyConflict = errors.New("idempotency key already used with different payment data")
)

type Status string

const (
	StatusPreparing Status = "preparing"
	StatusPending   Status = "pending"
	StatusPaid      Status = "paid"
	StatusCancelled Status = "cancelled"
	StatusFailed    Status = "failed"
	StatusUnknown   Status = "unknown"
)

type Project struct {
	ID         string
	APIKey     string
	StoreAlias string
	ReturnURL  string
}

type StoreConfig struct {
	Alias string
	ID    string
}

type Config struct {
	PublicBaseURL       string
	PayPhoneReturnPath  string
	PayPhoneResponseURL string
	Projects            map[string]Project
	Stores              map[string]StoreConfig
}

type Payment struct {
	ID                    string
	ProjectID             string
	OrderID               string
	Amount                int64
	Currency              string
	StoreAlias            string
	StoreID               string
	Reference             string
	IdempotencyKey        string
	ClientTransactionID   string
	PublicToken           string
	CheckoutURL           string
	PayWithCard           string
	PayWithPayPhone       string
	ProviderPaymentID     string
	ProviderTransactionID int64
	ReturnURL             string
	Status                Status
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type Repository interface {
	Reserve(context.Context, Payment) (Payment, bool, error)
	Update(context.Context, Payment) error
	ByID(context.Context, string) (Payment, error)
	ByPublicToken(context.Context, string) (Payment, error)
	ByClientTransactionID(context.Context, string) (Payment, error)
}

type MemoryRepository struct {
	mu            sync.RWMutex
	payments      map[string]Payment
	byIdempotency map[string]string
	byPublicToken map[string]string
	byClientTx    map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		payments:      make(map[string]Payment),
		byIdempotency: make(map[string]string),
		byPublicToken: make(map[string]string),
		byClientTx:    make(map[string]string),
	}
}

func (r *MemoryRepository) Reserve(_ context.Context, payment Payment) (Payment, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := payment.ProjectID + "\x00" + payment.IdempotencyKey
	if existingID, ok := r.byIdempotency[key]; ok {
		return clonePayment(r.payments[existingID]), false, nil
	}
	if _, ok := r.payments[payment.ID]; ok {
		return Payment{}, false, errors.New("payment id already exists")
	}
	if _, ok := r.byPublicToken[payment.PublicToken]; ok {
		return Payment{}, false, errors.New("public token already exists")
	}
	if _, ok := r.byClientTx[payment.ClientTransactionID]; ok {
		return Payment{}, false, errors.New("client transaction id already exists")
	}

	r.payments[payment.ID] = clonePayment(payment)
	r.byIdempotency[key] = payment.ID
	r.byPublicToken[payment.PublicToken] = payment.ID
	r.byClientTx[payment.ClientTransactionID] = payment.ID

	return clonePayment(payment), true, nil
}

func (r *MemoryRepository) Update(_ context.Context, payment Payment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.payments[payment.ID]; !ok {
		return ErrNotFound
	}
	r.payments[payment.ID] = clonePayment(payment)
	return nil
}

func (r *MemoryRepository) ByID(_ context.Context, id string) (Payment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	payment, ok := r.payments[id]
	if !ok {
		return Payment{}, ErrNotFound
	}
	return clonePayment(payment), nil
}

func (r *MemoryRepository) ByPublicToken(_ context.Context, token string) (Payment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byPublicToken[token]
	if !ok {
		return Payment{}, ErrNotFound
	}
	return clonePayment(r.payments[id]), nil
}

func (r *MemoryRepository) ByClientTransactionID(_ context.Context, clientTransactionID string) (Payment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byClientTx[clientTransactionID]
	if !ok {
		return Payment{}, ErrNotFound
	}
	return clonePayment(r.payments[id]), nil
}

func clonePayment(payment Payment) Payment {
	return payment
}

var _ Repository = (*MemoryRepository)(nil)
