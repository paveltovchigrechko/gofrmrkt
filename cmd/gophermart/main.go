package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/paveltovchigrechko/gofrmrkt/internal/config"
	"github.com/paveltovchigrechko/gofrmrkt/internal/middleware"
	"github.com/paveltovchigrechko/gofrmrkt/internal/repo"
	"github.com/paveltovchigrechko/gofrmrkt/internal/server"
	"go.uber.org/zap"
)

const logFilePath = "app.log"

func main() {
	l, err := middleware.NewLogger(logFilePath)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Sync()

	run(l)
}

func run(logger *zap.SugaredLogger) {
	cfg, err := config.CreateAppConfig(os.Args[1:])
	if err != nil {
		logger.Fatal(err)
	}

	storage, err := repo.NewPostgresDB(cfg.DatabaseURI)
	if err != nil {
		logger.Fatal(err)
	}

	serv, err := server.New(cfg, logger, storage)
	if err != nil {
		logger.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := serv.Run(ctx); err != nil {
		logger.Fatal(err)
	}
}
