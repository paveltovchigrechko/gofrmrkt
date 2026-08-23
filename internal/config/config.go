package config

import (
	"flag"
	"strings"

	"github.com/caarlos0/env/v6"
	"github.com/paveltovchigrechko/gofrmrkt/internal/apperrors"
)

const (
	addressFlag              = "a"
	databaseDSNFlag          = "d"
	accrualSystemAddressFlag = "r"
)

type AppConfig struct {
	Addr           string
	DatabaseURI    string
	AccrualSysAddr string
}

type envConfig struct {
	Addr           *string `env:"RUN_ADDRESS"`
	URI            *string `env:"DATABASE_URI"`
	AccrualSysAddr *string `env:"ACCRUAL_SYSTEM_ADDRESS"`
}

func CreateAppConfig(args []string) (*AppConfig, error) {
	envCfg, err := parseEnvConfig()
	if err != nil {
		return nil, err
	}

	cfg, err := parseFlags(args)
	if err != nil {
		return nil, err
	}

	if envCfg.Addr != nil {
		cfg.Addr = *envCfg.Addr
	} else if strings.Trim(cfg.Addr, " ") == "" {
		return nil, apperrors.ErrAddrEmpty
	}

	if envCfg.URI != nil {
		cfg.DatabaseURI = *envCfg.URI
	} else if strings.TrimSpace(cfg.DatabaseURI) == "" {
		return nil, apperrors.ErrDSNEmpty
	}

	if envCfg.AccrualSysAddr != nil {
		cfg.AccrualSysAddr = *envCfg.AccrualSysAddr
	} else if strings.TrimSpace(cfg.AccrualSysAddr) == "" {
		return nil, apperrors.ErrAccrualSysAddr
	}

	return cfg, nil
}

func parseFlags(args []string) (*AppConfig, error) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)

	address := fs.String(addressFlag, "", "Application server HTTP address")
	dbURI := fs.String(databaseDSNFlag, "", "Database URI")
	accrualSysAddr := fs.String(accrualSystemAddressFlag, "", "Accrual system HTTP address")

	err := fs.Parse(args)
	if err != nil {
		return nil, err
	}

	return &AppConfig{
		Addr:           *address,
		DatabaseURI:    *dbURI,
		AccrualSysAddr: *accrualSysAddr,
	}, nil

}

func parseEnvConfig() (*envConfig, error) {
	envCfg := &envConfig{}

	err := env.Parse(envCfg)
	if err != nil {
		return nil, err
	}

	// Treat empty environment variables as error.
	if err = validateEnvConfig(envCfg); err != nil {
		return nil, err
	}

	return envCfg, nil
}

func validateEnvConfig(cfg *envConfig) error {
	if cfg.Addr != nil && strings.TrimSpace(*cfg.Addr) == "" {
		return apperrors.ErrAddrEmpty
	}

	if cfg.URI != nil && strings.TrimSpace(*cfg.URI) == "" {
		return apperrors.ErrDSNEmpty
	}

	if cfg.AccrualSysAddr != nil && strings.TrimSpace(*cfg.AccrualSysAddr) == "" {
		return apperrors.ErrAccrualSysAddr
	}

	return nil
}
