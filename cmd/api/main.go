package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/api"
	"github.com/kevinle-00/transit-observatory/internal/config"
	"github.com/kevinle-00/transit-observatory/internal/database"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(ctx, os.Getenv, logger); err != nil {
		logger.Error("API failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, logger *slog.Logger) error {
	apiConfig, err := config.LoadAPI(getenv)
	if err != nil {
		return err
	}
	databaseConfig, err := config.LoadDatabase(getenv)
	if err != nil {
		return err
	}
	db, err := database.Open(ctx, databaseConfig.URL)
	if err != nil {
		return err
	}
	defer db.Close()

	listener, err := net.Listen("tcp", apiConfig.Address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", apiConfig.Address, err)
	}
	requestBaseContext, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	server := &http.Server{
		Handler: api.NewHandler(
			db, database.NewReadRepository(db), logger,
			apiConfig.CORSAllowedOrigin, apiConfig.RequestTimeout, database.StatusQuery{
				AlertDataMaxAge: apiConfig.StatusAlertDataMaxAge, AlertCheckMaxAge: apiConfig.StatusAlertCheckMaxAge,
				GTFSDataMaxAge: apiConfig.StatusGTFSDataMaxAge, GTFSCheckMaxAge: apiConfig.StatusGTFSCheckMaxAge,
				AlertRunMaxDuration: apiConfig.StatusAlertRunMaxDuration, GTFSRunMaxDuration: apiConfig.StatusGTFSRunMaxDuration,
				FutureTolerance: apiConfig.StatusFutureTolerance, RecentFailureLimit: apiConfig.StatusRecentFailureLimit,
			},
		),
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		BaseContext:       func(net.Listener) context.Context { return requestBaseContext },
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      apiConfig.RequestTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(listener)
	}()
	logger.Info("API listening", "address", listener.Addr().String())

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		if err := shutdownServer(server, cancelRequests, apiConfig.ShutdownTimeout); err != nil {
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		if err := <-serveErrors; !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP during shutdown: %w", err)
		}
		logger.Info("API stopped")
		return nil
	}
}

func shutdownServer(server *http.Server, cancelRequests context.CancelFunc, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		cancelRequests()
		return errors.Join(err, server.Close())
	}
	return nil
}
