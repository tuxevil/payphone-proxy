package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tuxevil/payphone-proxy/internal/config"
	"github.com/tuxevil/payphone-proxy/internal/httpapi"
	"github.com/tuxevil/payphone-proxy/internal/payments"
	"github.com/tuxevil/payphone-proxy/internal/payphone"
)

func main() {
	settings, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	if err := ensureDatabaseDirectory(settings.DatabasePath); err != nil {
		log.Fatal(err)
	}
	database, err := sql.Open("sqlite", settings.DatabasePath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	repository, err := payments.NewSQLiteRepository(database)
	if err != nil {
		log.Fatal(err)
	}
	provider := payphone.NewHTTPClient(settings.PayPhoneBaseURL, settings.PayPhoneToken, &http.Client{Timeout: 15 * time.Second})
	service := payments.NewService(settings.PaymentsConfig(), repository, provider)

	server := &http.Server{
		Addr:              settings.Addr,
		Handler:           httpapi.New(service),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-shutdownContext.Done()
		closeContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(closeContext); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("shutdown server: %v", err)
		}
	}()

	log.Printf("payphone-proxy listening on %s", settings.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func ensureDatabaseDirectory(databasePath string) error {
	if databasePath == ":memory:" || databasePath == "" || filepath.Dir(databasePath) == "." {
		return nil
	}
	return os.MkdirAll(filepath.Dir(databasePath), 0750)
}
