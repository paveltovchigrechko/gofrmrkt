package main

import (
	"log"
	"os"

	"github.com/paveltovchigrechko/gofrmrkt/internal/config"
	"github.com/paveltovchigrechko/gofrmrkt/internal/middleware"
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

	serv, err := server.New(cfg, middleware.Logger(logger))
	if err != nil {
		logger.Fatal(err)
	}
	err = serv.Run()
	if err != nil {
		logger.Fatal(err)
	}
}
