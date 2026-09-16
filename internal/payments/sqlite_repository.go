package payments

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteRepository struct {
	database *sql.DB
}

func NewSQLiteRepository(database *sql.DB) (*SQLiteRepository, error) {
	if database == nil {
		return nil, errors.New("sqlite database is nil")
	}
	repository := &SQLiteRepository{database: database}
	// SQLite allows concurrent readers, but this service uses a single local
	// database. A single connection plus a busy timeout makes lock acquisition
	// predictable and leaves the compare-and-swap transition below as the
	// authority for concurrent state changes.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if _, err := database.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return nil, fmt.Errorf("configure sqlite busy timeout: %w", err)
	}
	if _, err := database.Exec(`
CREATE TABLE IF NOT EXISTS payments (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    order_id TEXT NOT NULL,
    amount INTEGER NOT NULL,
    currency TEXT NOT NULL,
    store_alias TEXT NOT NULL,
    store_id TEXT NOT NULL,
    reference TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL,
    client_transaction_id TEXT NOT NULL UNIQUE,
    public_token TEXT NOT NULL UNIQUE,
    checkout_url TEXT NOT NULL,
    pay_with_card TEXT NOT NULL DEFAULT '',
    pay_with_payphone TEXT NOT NULL DEFAULT '',
    provider_payment_id TEXT NOT NULL DEFAULT '',
    provider_transaction_id INTEGER,
    return_url TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(project_id, idempotency_key)
);`); err != nil {
		return nil, fmt.Errorf("create payments schema: %w", err)
	}

	return repository, nil
}

func (r *SQLiteRepository) Reserve(ctx context.Context, payment Payment) (Payment, bool, error) {
	_, err := r.database.ExecContext(ctx, `
INSERT INTO payments (
    id, project_id, order_id, amount, currency, store_alias, store_id,
    reference, idempotency_key, client_transaction_id, public_token,
    checkout_url, pay_with_card, pay_with_payphone, provider_payment_id,
    provider_transaction_id, return_url, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, paymentValues(payment)...)
	if err == nil {
		return payment, true, nil
	}

	// SQLite reports the uniqueness race as an insert error. Looking up the
	// scoped idempotency key makes retries return the original payment.
	existing, lookupErr := r.byIdempotency(ctx, payment.ProjectID, payment.IdempotencyKey)
	if lookupErr == nil {
		return existing, false, nil
	}

	return Payment{}, false, fmt.Errorf("reserve payment: %w", err)
}

func (r *SQLiteRepository) Update(ctx context.Context, payment Payment) error {
	result, err := r.database.ExecContext(ctx, `
UPDATE payments SET
    project_id = ?, order_id = ?, amount = ?, currency = ?, store_alias = ?,
    store_id = ?, reference = ?, idempotency_key = ?, client_transaction_id = ?,
    public_token = ?, checkout_url = ?, pay_with_card = ?, pay_with_payphone = ?,
    provider_payment_id = ?, provider_transaction_id = ?, return_url = ?,
    status = ?, created_at = ?, updated_at = ?
WHERE id = ?
`, append(paymentValues(payment)[1:], payment.ID)...)
	if err != nil {
		return fmt.Errorf("update payment: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect payment update: %w", err)
	}
	if rows != 1 {
		return ErrNotFound
	}

	return nil
}

func (r *SQLiteRepository) UpdateIfStatus(ctx context.Context, payment Payment, expectedStatus Status) (bool, error) {
	values := paymentValues(payment)[1:]
	values = append(values, payment.ID, string(expectedStatus))
	result, err := r.database.ExecContext(ctx, `
UPDATE payments SET
    project_id = ?, order_id = ?, amount = ?, currency = ?, store_alias = ?,
    store_id = ?, reference = ?, idempotency_key = ?, client_transaction_id = ?,
    public_token = ?, checkout_url = ?, pay_with_card = ?, pay_with_payphone = ?,
    provider_payment_id = ?, provider_transaction_id = ?, return_url = ?,
    status = ?, created_at = ?, updated_at = ?
WHERE id = ? AND status = ?
`, values...)
	if err != nil {
		return false, fmt.Errorf("conditional payment update: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect conditional payment update: %w", err)
	}
	return rows == 1, nil
}

func (r *SQLiteRepository) ByID(ctx context.Context, id string) (Payment, error) {
	return r.queryOne(ctx, `SELECT `+paymentColumns+` FROM payments WHERE id = ?`, id)
}

func (r *SQLiteRepository) ByPublicToken(ctx context.Context, token string) (Payment, error) {
	return r.queryOne(ctx, `SELECT `+paymentColumns+` FROM payments WHERE public_token = ?`, token)
}

func (r *SQLiteRepository) ByClientTransactionID(ctx context.Context, clientTransactionID string) (Payment, error) {
	return r.queryOne(ctx, `SELECT `+paymentColumns+` FROM payments WHERE client_transaction_id = ?`, clientTransactionID)
}

func (r *SQLiteRepository) byIdempotency(ctx context.Context, projectID, idempotencyKey string) (Payment, error) {
	return r.queryOne(ctx, `SELECT `+paymentColumns+` FROM payments WHERE project_id = ? AND idempotency_key = ?`, projectID, idempotencyKey)
}

const paymentColumns = `id, project_id, order_id, amount, currency, store_alias, store_id,
reference, idempotency_key, client_transaction_id, public_token, checkout_url,
pay_with_card, pay_with_payphone, provider_payment_id, provider_transaction_id,
return_url, status, created_at, updated_at`

func (r *SQLiteRepository) queryOne(ctx context.Context, query string, args ...any) (Payment, error) {
	row := r.database.QueryRowContext(ctx, query, args...)
	var payment Payment
	var providerTransactionID sql.NullInt64
	var createdAt, updatedAt string
	if err := row.Scan(
		&payment.ID,
		&payment.ProjectID,
		&payment.OrderID,
		&payment.Amount,
		&payment.Currency,
		&payment.StoreAlias,
		&payment.StoreID,
		&payment.Reference,
		&payment.IdempotencyKey,
		&payment.ClientTransactionID,
		&payment.PublicToken,
		&payment.CheckoutURL,
		&payment.PayWithCard,
		&payment.PayWithPayPhone,
		&payment.ProviderPaymentID,
		&providerTransactionID,
		&payment.ReturnURL,
		&payment.Status,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Payment{}, ErrNotFound
		}
		return Payment{}, fmt.Errorf("query payment: %w", err)
	}
	if providerTransactionID.Valid {
		payment.ProviderTransactionID = providerTransactionID.Int64
	}
	payment.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	payment.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

	return payment, nil
}

func paymentValues(payment Payment) []any {
	var providerTransactionID any
	if payment.ProviderTransactionID != 0 {
		providerTransactionID = payment.ProviderTransactionID
	}

	return []any{
		payment.ID,
		payment.ProjectID,
		payment.OrderID,
		payment.Amount,
		payment.Currency,
		payment.StoreAlias,
		payment.StoreID,
		payment.Reference,
		payment.IdempotencyKey,
		payment.ClientTransactionID,
		payment.PublicToken,
		payment.CheckoutURL,
		payment.PayWithCard,
		payment.PayWithPayPhone,
		payment.ProviderPaymentID,
		providerTransactionID,
		payment.ReturnURL,
		string(payment.Status),
		payment.CreatedAt.UTC().Format(time.RFC3339Nano),
		payment.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

var _ Repository = (*SQLiteRepository)(nil)
