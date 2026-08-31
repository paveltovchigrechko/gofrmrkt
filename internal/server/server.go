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

const (
	defaultTokenTimeToLive = time.Hour * 24
)

type Server struct {
	config   *config.AppConfig
	router   *chi.Mux
	handler  *handler.AppHandler
	database *repo.Postgres
}

func New(cfg *config.AppConfig, logger *zap.SugaredLogger, middlewares ...func(http.Handler) http.Handler) (*Server, error) {
	postgresDB, err := repo.NewPostgresDB(cfg.DatabaseURI)
	if err != nil {
		logger.Errorw(err.Error())
		return nil, err
	}
	authService, err := service.NewAuthService(cfg.SecretKey, defaultTokenTimeToLive)
	if err != nil {
		logger.Errorw(err.Error())
		return nil, err
	}

	h := handler.New(postgresDB, logger, authService)

	r := chi.NewRouter()
	r.Use(middlewares...) // Middlewares order: figure out
	// Public routers
	r.Post("/api/user/register", h.RegisterUser)
	r.Post("/api/user/login", h.AuthenticateUser)

	// Prepare authentication middleware

	authenticator := middleware.NewAuthenticator(authService)

	// Private routes: require authentication middleware
	r.Group(func(r chi.Router) {
		r.Use(authenticator.UserIDMiddleware)
		// r.Post("/api/user/orders", h.UploadOrder)
		// r.Get("/api/user/orders", h.GetOrders)
		// r.Get("/api/user/balance", h.GetBalance)
		// r.Post("/api/user/balance/withdraw", h.WithdrawBalance)
		// r.Get("/api/user/withdrawals", h.GetWithdrawals)
	})

	s := &Server{
		config:   cfg,
		router:   r,
		handler:  h,
		database: postgresDB,
	}

	return s, nil
}

func (s *Server) Run() error {
	return http.ListenAndServe(s.config.Addr, s.router)
}

func (s *Server) CloseDB() error {
	return s.database.Close()
}
