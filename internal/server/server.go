package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/paveltovchigrechko/gofrmrkt/internal/accrual"
	"github.com/paveltovchigrechko/gofrmrkt/internal/config"
	"github.com/paveltovchigrechko/gofrmrkt/internal/handler"
	"github.com/paveltovchigrechko/gofrmrkt/internal/middleware"
	"github.com/paveltovchigrechko/gofrmrkt/internal/repo"
	"github.com/paveltovchigrechko/gofrmrkt/internal/service"
	"github.com/paveltovchigrechko/gofrmrkt/internal/worker"
	"go.uber.org/zap"
)

const (
	defaultTokenTimeToLive = time.Hour * 24
	shutdownTimeout        = 15 * time.Second
)

type Server struct {
	config     *config.AppConfig
	httpServer *http.Server
	handler    *handler.AppHandler
	storage    repo.Storage
	worker     *worker.AccrualWorker
	logger     *zap.SugaredLogger
}

func New(cfg *config.AppConfig, logger *zap.SugaredLogger, storage repo.Storage) (*Server, error) {
	authService, err := service.NewAuthService(cfg.SecretKey, defaultTokenTimeToLive)
	if err != nil {
		return nil, err
	}

	h := handler.New(storage, logger, authService)

	r := chi.NewRouter()
	r.Use(middleware.GZIPMiddleware)
	r.Use(middleware.Logger(logger))

	r.Post("/api/user/register", h.RegisterUser)
	r.Post("/api/user/login", h.AuthenticateUser)

	authenticator := middleware.NewAuthenticator(authService)
	r.Group(func(r chi.Router) {
		r.Use(authenticator.UserIDMiddleware)
		r.Post("/api/user/orders", h.UploadOrder)
		r.Get("/api/user/orders", h.GetOrders)
		r.Get("/api/user/balance", h.GetBalance)
		r.Post("/api/user/balance/withdraw", h.WithdrawBalance)
		r.Get("/api/user/withdrawals", h.GetWithdrawals)
	})

	accrualClient := accrual.NewClient(cfg.AccrualSysAddr)
	accrualWorker := worker.NewAccrualWorker(storage, accrualClient, logger)

	return &Server{
		config:     cfg,
		httpServer: &http.Server{Addr: cfg.Addr, Handler: r},
		handler:    h,
		storage:    storage,
		worker:     accrualWorker,
		logger:     logger,
	}, nil
}

// Run starts the HTTP server and the accrual worker, and blocks until ctx
// is canceled or the HTTP server fails to start. On return, both the
// worker and the storage connection are guaranteed to be fully stopped —
// callers don't need their own cleanup beyond calling Run.
func (s *Server) Run(ctx context.Context) error {
	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.worker.Run(workerCtx)
	}()

	serveErrCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrCh <- err
			return
		}
		serveErrCh <- nil
	}()

	var runErr error
	select {
	case <-ctx.Done():
		s.logger.Infow("shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Errorw("http server shutdown", "error", err)
		}
	case err := <-serveErrCh:
		runErr = err
	}

	// Ensure the worker stops on every exit path, not just the ctx.Done()
	// (signal-triggered) one — e.g. if ListenAndServe failed on startup.
	cancelWorker()
	wg.Wait()

	if err := s.storage.Close(); err != nil {
		s.logger.Errorw("close storage", "error", err)
	}

	return runErr
}
