package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/paveltovchigrechko/gofrmrkt/internal/config"
	"github.com/paveltovchigrechko/gofrmrkt/internal/handler"
	"github.com/paveltovchigrechko/gofrmrkt/internal/middleware"
	"github.com/paveltovchigrechko/gofrmrkt/internal/repo"
	"github.com/paveltovchigrechko/gofrmrkt/internal/service"
	"go.uber.org/zap"
)

const defaultTokenTimeToLive = time.Hour * 24

type Server struct {
	config  *config.AppConfig
	router  *chi.Mux
	handler *handler.AppHandler
	storage repo.Storage
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

	return &Server{
		config:  cfg,
		router:  r,
		handler: h,
		storage: storage,
	}, nil
}

func (s *Server) Run() error {
	return http.ListenAndServe(s.config.Addr, s.router)
}

func (s *Server) CloseDB() error {
	return s.storage.Close()
}
