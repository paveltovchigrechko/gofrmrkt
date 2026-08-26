package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/paveltovchigrechko/gofrmrkt/internal/config"
	"github.com/paveltovchigrechko/gofrmrkt/internal/db"
	"github.com/paveltovchigrechko/gofrmrkt/internal/handler"
)

type Server struct {
	config   *config.AppConfig
	router   *chi.Mux
	handler  *handler.AppHandler
	database *db.Postgres
}

func New(cfg *config.AppConfig, middlewares ...func(http.Handler) http.Handler) (*Server, error) {
	postgresDB, err := db.NewPostgresDB(cfg.DatabaseURI)
	if err != nil {
		return nil, err
	}

	h := handler.New(postgresDB)

	r := chi.NewRouter()
	r.Use(middlewares...)
	// Public routers
	r.Post("/api/user/register", h.RegisterUser)
	r.Post("/api/user/login", h.AuthenticateUser)

	// Private routes: require authentication
	r.Group(func(r chi.Router) {
		// r.Use(middleware.Authentication)
		r.Post("/api/user/orders", h.UploadOrder)
		r.Get("/api/user/orders", h.GetOrders)
		r.Get("/api/user/balance", h.GetBalance)
		r.Post("/api/user/balance/withdraw", h.WithdrawBalance)
		r.Get("/api/user/withdrawals", h.GetWithdrawals)
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
