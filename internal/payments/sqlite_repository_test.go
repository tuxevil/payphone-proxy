package payments_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/tuxevil/payphone-proxy/internal/payments"
	_ "modernc.org/sqlite"
)

func TestSQLiteRepositoryPersistsPaymentAndIdempotencyIndex(t *testing.T) {
	database, err := sql.Open("sqlite", "file:payments-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer database.Close()

	repository, err := payments.NewSQLiteRepository(database)
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	payment := payments.Payment{
		ID:                  "pay_test",
		ProjectID:           "project-a",
		OrderID:             "order-1",
		Amount:              100,
		Currency:            "USD",
		StoreAlias:          "store-a",
		StoreID:             "store-id-a",
		IdempotencyKey:      "request-1",
		ClientTransactionID: "client-1",
		PublicToken:         "public-1",
		CheckoutURL:         "https://proxy.example.test/checkout/public-1",
		ReturnURL:           "https://project.example.test/return",
		Status:              payments.StatusPreparing,
		CreatedAt:           time.Date(2026, 9, 15, 22, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 9, 15, 22, 0, 0, 0, time.UTC),
	}
	reserved, created, err := repository.Reserve(context.Background(), payment)
	if err != nil || !created || reserved.ID != payment.ID {
		t.Fatalf("reserve = %#v, created=%v, err=%v", reserved, created, err)
	}

	secondRepository, err := payments.NewSQLiteRepository(database)
	if err != nil {
		t.Fatalf("reopen repository: %v", err)
	}
	persisted, err := secondRepository.ByID(context.Background(), payment.ID)
	if err != nil {
		t.Fatalf("load persisted payment: %v", err)
	}
	if persisted.OrderID != payment.OrderID || persisted.Amount != payment.Amount || persisted.Status != payment.Status {
		t.Fatalf("persisted payment = %#v, want %#v", persisted, payment)
	}

	duplicate, created, err := secondRepository.Reserve(context.Background(), payment)
	if err != nil || created || duplicate.ID != payment.ID {
		t.Fatalf("duplicate reserve = %#v, created=%v, err=%v", duplicate, created, err)
	}
}

func TestSQLiteRepositoryConditionalUpdatePreventsStaleTransition(t *testing.T) {
	database, err := sql.Open("sqlite", "file:conditional-update-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer database.Close()
	repository, err := payments.NewSQLiteRepository(database)
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	payment := payments.Payment{
		ID:                  "pay_conditional",
		ProjectID:           "project-a",
		OrderID:             "order-conditional",
		Amount:              100,
		Currency:            "USD",
		StoreAlias:          "store-a",
		StoreID:             "store-id-a",
		IdempotencyKey:      "request-conditional",
		ClientTransactionID: "client-conditional",
		PublicToken:         "public-conditional",
		CheckoutURL:         "https://proxy.example.test/checkout/public-conditional",
		Status:              payments.StatusPreparing,
		CreatedAt:           time.Now().UTC(),
		UpdatedAt:           time.Now().UTC(),
	}
	if _, created, err := repository.Reserve(context.Background(), payment); err != nil || !created {
		t.Fatalf("reserve created=%v err=%v", created, err)
	}
	pending := payment
	pending.Status = payments.StatusPending
	pending.UpdatedAt = time.Now().UTC()
	updated, err := repository.UpdateIfStatus(context.Background(), pending, payments.StatusPreparing)
	if err != nil || !updated {
		t.Fatalf("first conditional update = %v, err=%v", updated, err)
	}
	stale := pending
	stale.Status = payments.StatusCancelled
	stale.UpdatedAt = time.Now().UTC()
	updated, err = repository.UpdateIfStatus(context.Background(), stale, payments.StatusPreparing)
	if err != nil || updated {
		t.Fatalf("stale conditional update = %v, err=%v; want false", updated, err)
	}
	current, err := repository.ByID(context.Background(), payment.ID)
	if err != nil || current.Status != payments.StatusPending {
		t.Fatalf("current status = %q, err=%v; want pending", current.Status, err)
	}
}
