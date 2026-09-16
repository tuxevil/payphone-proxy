package payments_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tuxevil/payphone-proxy/internal/payments"
	"github.com/tuxevil/payphone-proxy/internal/payphone"
	_ "modernc.org/sqlite"
)

type securityProvider struct {
	mu             sync.Mutex
	prepareRequest payphone.PrepareRequest
	prepareErr     error
	prepare        payphone.PrepareResponse
	prepareHook    func(context.Context, payphone.PrepareRequest)
	confirmCalls   atomic.Int32
	confirmHook    func()
	confirm        func(payphone.ConfirmRequest) (payphone.ConfirmResponse, error)
}

func (p *securityProvider) Prepare(ctx context.Context, request payphone.PrepareRequest) (payphone.PrepareResponse, error) {
	p.mu.Lock()
	p.prepareRequest = request
	err, response, hook := p.prepareErr, p.prepare, p.prepareHook
	p.mu.Unlock()
	if hook != nil {
		hook(ctx, request)
	}
	if err != nil {
		return payphone.PrepareResponse{}, err
	}
	if response.PaymentID == "" {
		response = payphone.PrepareResponse{
			PaymentID:   "provider-payment",
			PayWithCard: "https://pay.payphonetodoesposible.com/Anonymous/Index?paymentId=provider-payment",
		}
	}
	return response, nil
}

func (p *securityProvider) Confirm(_ context.Context, request payphone.ConfirmRequest) (payphone.ConfirmResponse, error) {
	p.confirmCalls.Add(1)
	p.mu.Lock()
	hook := p.confirmHook
	confirm := p.confirm
	p.mu.Unlock()
	if hook != nil {
		hook()
	}
	return confirm(request)
}

func securityService(provider payphone.Client) (*payments.Service, *payments.MemoryRepository, payments.Project) {
	repository := payments.NewMemoryRepository()
	project := payments.Project{ID: "project", APIKey: "project-key", StoreAlias: "store"}
	service := payments.NewService(payments.Config{
		PublicBaseURL: "https://proxy.example.test",
		Projects:      map[string]payments.Project{project.ID: project},
		Stores:        map[string]payments.StoreConfig{project.StoreAlias: {Alias: "store", ID: "store-id"}},
	}, repository, provider)
	return service, repository, project
}

func approvedConfirmation(request payphone.ConfirmRequest, status int, storeID string) payphone.ConfirmResponse {
	return payphone.ConfirmResponse{
		ClientTransactionID: request.ClientTransactionID,
		TransactionID:       request.ID,
		StatusCode:          status,
		Amount:              100,
		Currency:            "USD",
		StoreID:             storeID,
	}
}

func TestTransientPrepareIsUnknownAndCanBeConfirmedLater(t *testing.T) {
	provider := &securityProvider{prepareErr: &payphone.HTTPError{StatusCode: 503}}
	service, repository, project := securityService(provider)
	_, _, err := service.Create(context.Background(), project, "retry-key", payments.CreateInput{OrderID: "order", Amount: 100, Currency: "USD"})
	if !errors.Is(err, payments.ErrProvider) {
		t.Fatalf("Create error = %v, want provider error", err)
	}

	provider.mu.Lock()
	transactionID := provider.prepareRequest.ClientTransactionID
	provider.prepareErr = nil
	provider.confirm = func(request payphone.ConfirmRequest) (payphone.ConfirmResponse, error) {
		return approvedConfirmation(request, 3, "store-id"), nil
	}
	provider.mu.Unlock()
	payment, err := repository.ByClientTransactionID(context.Background(), transactionID)
	if err != nil {
		t.Fatalf("load uncertain payment: %v", err)
	}
	if payment.Status != payments.StatusUnknown {
		t.Fatalf("status after 503 = %q, want unknown", payment.Status)
	}
	settled, err := service.HandleReturn(context.Background(), 41, transactionID)
	if err != nil || settled.Status != payments.StatusPaid {
		t.Fatalf("HandleReturn = %#v, err=%v", settled, err)
	}
}

func TestCancelledRequestStillPersistsUnknownState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &securityProvider{}
	provider.prepareErr = context.Canceled
	provider.confirm = func(payphone.ConfirmRequest) (payphone.ConfirmResponse, error) {
		return payphone.ConfirmResponse{}, errors.New("not called")
	}
	// Cancel before returning the provider error.
	provider.prepare = payphone.PrepareResponse{}
	service, repository, project := securityService(provider)
	provider.mu.Lock()
	provider.prepareErr = context.Canceled
	provider.prepareHook = func(context.Context, payphone.PrepareRequest) { cancel() }
	provider.mu.Unlock()
	_, _, err := service.Create(ctx, project, "cancel-key", payments.CreateInput{OrderID: "order", Amount: 100, Currency: "USD"})
	if !errors.Is(err, payments.ErrProvider) {
		t.Fatalf("Create error = %v, want provider error", err)
	}
	provider.mu.Lock()
	transactionID := provider.prepareRequest.ClientTransactionID
	provider.mu.Unlock()
	payment, err := repository.ByClientTransactionID(context.Background(), transactionID)
	if err != nil {
		t.Fatalf("load cancelled payment: %v", err)
	}
	if payment.Status != payments.StatusUnknown {
		t.Fatalf("status after cancelled request = %q, want unknown", payment.Status)
	}
}

func TestConcurrentReturnsCannotDowngradePaid(t *testing.T) {
	provider := &securityProvider{}
	provider.confirm = func(request payphone.ConfirmRequest) (payphone.ConfirmResponse, error) {
		return approvedConfirmation(request, 3, "store-id"), nil
	}
	service, repository, project := securityService(provider)
	payment, _, err := service.Create(context.Background(), project, "concurrent-key", payments.CreateInput{OrderID: "order", Amount: 100, Currency: "USD"})
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}

	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := service.HandleReturn(context.Background(), 41, payment.ClientTransactionID); err != nil {
				t.Errorf("HandleReturn error = %v", err)
			}
		}()
	}
	group.Wait()
	settled, err := repository.ByID(context.Background(), payment.ID)
	if err != nil || settled.Status != payments.StatusPaid {
		t.Fatalf("final payment = %#v, err=%v", settled, err)
	}
	if got := provider.confirmCalls.Load(); got != 1 {
		t.Fatalf("Confirm calls = %d, want 1", got)
	}
}

func TestSeparateServicesCannotDowngradePaidWithSQLiteCAS(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "payments.db")
	databaseA, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open database A: %v", err)
	}
	defer databaseA.Close()
	databaseB, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open database B: %v", err)
	}
	defer databaseB.Close()
	repositoryA, err := payments.NewSQLiteRepository(databaseA)
	if err != nil {
		t.Fatalf("create repository A: %v", err)
	}
	repositoryB, err := payments.NewSQLiteRepository(databaseB)
	if err != nil {
		t.Fatalf("create repository B: %v", err)
	}
	project := payments.Project{ID: "project", APIKey: "project-key", StoreAlias: "store"}
	configuration := payments.Config{
		PublicBaseURL: "https://proxy.example.test",
		Projects:      map[string]payments.Project{project.ID: project},
		Stores:        map[string]payments.StoreConfig{project.StoreAlias: {Alias: "store", ID: "store-id"}},
	}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstProvider := &securityProvider{}
	firstProvider.confirm = func(request payphone.ConfirmRequest) (payphone.ConfirmResponse, error) {
		close(firstStarted)
		<-releaseFirst
		return approvedConfirmation(request, 2, "store-id"), nil
	}
	secondProvider := &securityProvider{}
	secondProvider.confirm = func(request payphone.ConfirmRequest) (payphone.ConfirmResponse, error) {
		return approvedConfirmation(request, 3, "store-id"), nil
	}
	serviceA := payments.NewService(configuration, repositoryA, firstProvider)
	serviceB := payments.NewService(configuration, repositoryB, secondProvider)
	payment, _, err := serviceA.Create(context.Background(), project, "multi-process-key", payments.CreateInput{OrderID: "order", Amount: 100, Currency: "USD"})
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := serviceA.HandleReturn(context.Background(), 41, payment.ClientTransactionID)
		firstDone <- err
	}()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first provider call did not start")
	}
	secondDone := make(chan error, 1)
	go func() {
		settled, err := serviceB.HandleReturn(context.Background(), 41, payment.ClientTransactionID)
		if err == nil && settled.Status != payments.StatusPaid {
			err = errors.New("second service did not observe paid state")
		}
		secondDone <- err
	}()
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	final, err := repositoryA.ByID(context.Background(), payment.ID)
	if err != nil || final.Status != payments.StatusPaid {
		t.Fatalf("final payment = %#v, err=%v", final, err)
	}
}

func TestSeparateServicesDoNotLosePaidBehindUnknownWithSQLiteCAS(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "payments.db")
	databaseA, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open database A: %v", err)
	}
	defer databaseA.Close()
	databaseB, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open database B: %v", err)
	}
	defer databaseB.Close()
	repositoryA, err := payments.NewSQLiteRepository(databaseA)
	if err != nil {
		t.Fatalf("create repository A: %v", err)
	}
	repositoryB, err := payments.NewSQLiteRepository(databaseB)
	if err != nil {
		t.Fatalf("create repository B: %v", err)
	}
	project := payments.Project{ID: "project", APIKey: "project-key", StoreAlias: "store"}
	configuration := payments.Config{
		PublicBaseURL: "https://proxy.example.test",
		Projects:      map[string]payments.Project{project.ID: project},
		Stores:        map[string]payments.StoreConfig{project.StoreAlias: {Alias: "store", ID: "store-id"}},
	}
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	firstProvider := &securityProvider{}
	firstProvider.confirm = func(request payphone.ConfirmRequest) (payphone.ConfirmResponse, error) {
		return approvedConfirmation(request, 1, "store-id"), nil
	}
	secondProvider := &securityProvider{}
	secondProvider.confirmHook = func() {
		close(secondStarted)
		<-releaseSecond
	}
	secondProvider.confirm = func(request payphone.ConfirmRequest) (payphone.ConfirmResponse, error) {
		return approvedConfirmation(request, 3, "store-id"), nil
	}
	serviceA := payments.NewService(configuration, repositoryA, firstProvider)
	serviceB := payments.NewService(configuration, repositoryB, secondProvider)
	payment, _, err := serviceA.Create(context.Background(), project, "unknown-race-key", payments.CreateInput{OrderID: "order", Amount: 100, Currency: "USD"})
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	secondDone := make(chan error, 1)
	go func() {
		settled, err := serviceB.HandleReturn(context.Background(), 41, payment.ClientTransactionID)
		if err == nil && settled.Status != payments.StatusPaid {
			err = errors.New("second service did not upgrade unknown to paid")
		}
		secondDone <- err
	}()
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second provider call did not start")
	}
	unknown, err := serviceA.HandleReturn(context.Background(), 41, payment.ClientTransactionID)
	if err != nil || unknown.Status != payments.StatusUnknown {
		t.Fatalf("unknown transition = %#v, err=%v", unknown, err)
	}
	close(releaseSecond)
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	final, err := repositoryA.ByID(context.Background(), payment.ID)
	if err != nil || final.Status != payments.StatusPaid {
		t.Fatalf("final payment = %#v, err=%v", final, err)
	}
}

func TestConfirmationFromDifferentStoreIsRejected(t *testing.T) {
	provider := &securityProvider{}
	provider.confirm = func(request payphone.ConfirmRequest) (payphone.ConfirmResponse, error) {
		return approvedConfirmation(request, 3, "another-store"), nil
	}
	service, repository, project := securityService(provider)
	payment, _, err := service.Create(context.Background(), project, "store-key", payments.CreateInput{OrderID: "order", Amount: 100, Currency: "USD"})
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	_, err = service.HandleReturn(context.Background(), 41, payment.ClientTransactionID)
	if !errors.Is(err, payments.ErrInvalidConfirmation) {
		t.Fatalf("HandleReturn error = %v, want confirmation mismatch", err)
	}
	current, err := repository.ByID(context.Background(), payment.ID)
	if err != nil || current.Status != payments.StatusPending {
		t.Fatalf("payment after mismatch = %#v, err=%v", current, err)
	}
}

func TestUnrecognizedProviderStatusRemainsUnknown(t *testing.T) {
	provider := &securityProvider{}
	provider.confirm = func(request payphone.ConfirmRequest) (payphone.ConfirmResponse, error) {
		return approvedConfirmation(request, 1, "store-id"), nil
	}
	service, _, project := securityService(provider)
	payment, _, err := service.Create(context.Background(), project, "status-key", payments.CreateInput{OrderID: "order", Amount: 100, Currency: "USD"})
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	unknown, err := service.HandleReturn(context.Background(), 41, payment.ClientTransactionID)
	if err != nil || unknown.Status != payments.StatusUnknown {
		t.Fatalf("unknown transition = %#v, err=%v", unknown, err)
	}
	provider.mu.Lock()
	provider.confirm = func(request payphone.ConfirmRequest) (payphone.ConfirmResponse, error) {
		return approvedConfirmation(request, 3, "store-id"), nil
	}
	provider.mu.Unlock()
	paid, err := service.HandleReturn(context.Background(), 41, payment.ClientTransactionID)
	if err != nil || paid.Status != payments.StatusPaid {
		t.Fatalf("retry transition = %#v, err=%v", paid, err)
	}
}
