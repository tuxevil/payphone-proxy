package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
		// Keep configuration details out of startup logs because they may contain
		// sensitive values.
		log.Fatal("invalid configuration")
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
		MaxHeaderBytes:    16 * 1024,
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
	if databasePath == ":memory:" || databasePath == "" || strings.HasPrefix(databasePath, "file:") {
		return nil
	}
	databaseDirectory := filepath.Dir(databasePath)
	if err := os.MkdirAll(databaseDirectory, 0750); err != nil {
		return err
	}
	// Tighten the leaf directory when it is managed by this process. Avoid
	// chmod-ing the current directory for a bare relative database filename.
	if databaseDirectory != "." && databaseDirectory != string(filepath.Separator) {
		if err := os.Chmod(databaseDirectory, 0750); err != nil {
			return fmt.Errorf("restrict database directory: %w", err)
		}
	}
	database, err := os.OpenFile(databasePath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("create database file: %w", err)
	}
	if err := database.Chmod(0600); err != nil {
		_ = database.Close()
		return fmt.Errorf("restrict database file: %w", err)
	}
	if err := database.Close(); err != nil {
		return fmt.Errorf("close database file: %w", err)
	}
	return nil
}
