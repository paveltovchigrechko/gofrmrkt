package server

import (
	"database/sql"
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
	database *sql.DB
}

func New(cfg *config.AppConfig, middlewares ...func(http.Handler) http.Handler) (*Server, error) {
	r := chi.NewRouter()
	h := handler.New()

	postgres, err := db.OpenDB(cfg.DatabaseURI)
	if err != nil {
		return nil, err
	}
	err = db.RunMigrations(postgres)
	if err != nil {
		postgres.Close()
		return nil, err
	}

	s := &Server{
		config:   cfg,
		router:   r,
		handler:  h,
		database: postgres,
	}

	s.setHandlers()
	s.router.Use(middlewares...)

	return s, nil
}

func (s *Server) Run() error {
	return http.ListenAndServe(":1234", s.router)
}

func (s *Server) setHandlers() {
	s.router.Post("/api/user/register", s.handler.RegisterUser)
	s.router.Post("/api/user/login", s.handler.AuthenticateUser)
}
