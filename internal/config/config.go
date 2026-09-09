package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/caarlos0/env/v6"
)

const (
	addressFlag              = "a"
	databaseDSNFlag          = "d"
	accrualSystemAddressFlag = "r"
	secretKeyFlag            = "k"
)

var (
	errAddrEmpty      = errors.New("server address is empty")
	errDSNEmpty       = errors.New("database DSN is empty")
	errAccrualSysAddr = errors.New("accrual system address is empty")
	errSecretKeyEmpty = errors.New("secret key is empty")
)

type AppConfig struct {
	Addr           string
	DatabaseURI    string
	AccrualSysAddr string
	SecretKey      string
}

type envConfig struct {
	Addr           *string `env:"RUN_ADDRESS"`
	URI            *string `env:"DATABASE_URI"`
	AccrualSysAddr *string `env:"ACCRUAL_SYSTEM_ADDRESS"`
	SecretKey      *string `env:"SECRET_KEY"`
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
		return nil, errAddrEmpty
	}

	if envCfg.URI != nil {
		cfg.DatabaseURI = *envCfg.URI
	} else if strings.TrimSpace(cfg.DatabaseURI) == "" {
		return nil, errDSNEmpty
	}

	if envCfg.AccrualSysAddr != nil {
		cfg.AccrualSysAddr = *envCfg.AccrualSysAddr
	} else if strings.TrimSpace(cfg.AccrualSysAddr) == "" {
		return nil, errAccrualSysAddr
	}

	if envCfg.SecretKey != nil {
		cfg.SecretKey = *envCfg.SecretKey
	} else if strings.TrimSpace(cfg.SecretKey) == "" {
		generated, err := generateSecretKey()
		if err != nil {
			return nil, err
		}
		cfg.SecretKey = generated
	}

	return cfg, nil
}

func parseFlags(args []string) (*AppConfig, error) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)

	address := fs.String(addressFlag, "", "Application server HTTP address")
	dbURI := fs.String(databaseDSNFlag, "", "Database URI")
	accrualSysAddr := fs.String(accrualSystemAddressFlag, "", "Accrual system HTTP address")
	secretKey := fs.String(secretKeyFlag, "", "Secret key for encrypting")

	err := fs.Parse(args)
	if err != nil {
		return nil, err
	}

	return &AppConfig{
		Addr:           *address,
		DatabaseURI:    *dbURI,
		AccrualSysAddr: *accrualSysAddr,
		SecretKey:      *secretKey,
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
		return errAddrEmpty
	}

	if cfg.URI != nil && strings.TrimSpace(*cfg.URI) == "" {
		return errDSNEmpty
	}

	if cfg.AccrualSysAddr != nil && strings.TrimSpace(*cfg.AccrualSysAddr) == "" {
		return errAccrualSysAddr
	}

	if cfg.SecretKey != nil && strings.TrimSpace(*cfg.SecretKey) == "" {
		return errSecretKeyEmpty
	}

	return nil
}

func generateSecretKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret key: %w", err)
	}
	return hex.EncodeToString(b), nil
}
