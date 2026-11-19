package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ledger/internal/httpapi"
	"ledger/internal/ledger"
	"ledger/internal/postgres"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// run is the composition root: the only place that knows the concrete implementations.
func run() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	address := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		address = ":" + port
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Connect(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool); err != nil {
		return err
	}

	repository := postgres.NewExpenseRepository(pool)
	httpServer := &http.Server{
		Addr: address,
		Handler: httpapi.NewHandler(
			ledger.NewExpenseRegistry(repository, time.Now),
			ledger.NewPaymentForecaster(repository, time.Now),
		),
		ReadHeaderTimeout: 5 * time.Second,
	}

	servers := []*http.Server{httpServer}
	if pprofAddress := os.Getenv("PPROF_ADDR"); pprofAddress != "" {
		pprofServer := newPprofServer(pprofAddress)
		servers = append(servers, pprofServer)
		go func() {
			slog.Info("pprof listening", "address", pprofAddress)
			if err := pprofServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				slog.Error("pprof", "err", err) // profiling is optional: log it, keep the API up
			}
		}()
	}

	// Graceful shutdown: stop accepting, let in-flight requests finish, then close the pool.
	shutdownFinished := make(chan struct{})
	go func() {
		defer close(shutdownFinished)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, server := range servers {
			if err := server.Shutdown(shutdownCtx); err != nil {
				slog.Error("shutdown", "addr", server.Addr, "err", err)
			}
		}
	}()

	slog.Info("listening", "address", address)
	err = httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownFinished // ListenAndServe returns as soon as Shutdown starts; wait for it to drain
		return nil
	}
	return err
}
