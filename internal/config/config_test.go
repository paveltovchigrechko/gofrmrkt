package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAppConfig_Positive(t *testing.T) {
	testCases := []struct {
		name               string
		args               []string
		envVars            map[string]string
		wantAddr           string
		wantDbURI          string
		wantAccrualSysAddr string
	}{
		{
			name:               "Flags are parsed correctly when no env is present",
			args:               []string{"-a", "127.0.0.1:4444", "-d", "db_address", "-r", "accrual_system_address"},
			envVars:            map[string]string{},
			wantAddr:           "127.0.0.1:4444",
			wantDbURI:          "db_address",
			wantAccrualSysAddr: "accrual_system_address",
		},
		{
			name: "Env variables override flags completely",
			args: []string{"-a", "127.0.0.1:4444", "-d", "db_address", "-r", "accrual_system_address"},
			envVars: map[string]string{
				"RUN_ADDRESS":            "some-address",
				"DATABASE_URI":           "some-database-uri",
				"ACCRUAL_SYSTEM_ADDRESS": "some-accrual-system-address",
			},
			wantAddr:           "some-address",
			wantDbURI:          "some-database-uri",
			wantAccrualSysAddr: "some-accrual-system-address",
		},
		{
			name: "Partial Env overrides only specific flags",
			args: []string{"-a", "127.0.0.1:4444", "-d", "db_address", "-r", "accrual_system_address"},
			envVars: map[string]string{
				"RUN_ADDRESS": "some-address",
			},
			wantAddr:           "some-address",
			wantDbURI:          "db_address",
			wantAccrualSysAddr: "accrual_system_address",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RUN_ADDRESS", "")
			t.Setenv("DATABASE_URI", "")
			t.Setenv("ACCRUAL_SYSTEM_ADDRESS", "")
			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			cfg, err := CreateAppConfig(tc.args)
			require.NoError(t, err)
			require.NotNil(t, cfg)

			assert.Equal(t, tc.wantAddr, cfg.Addr)
			assert.Equal(t, tc.wantDbURI, cfg.DatabaseURI)
			assert.Equal(t, tc.wantAccrualSysAddr, cfg.AccrualSysAddr)
		})
	}
}

func TestCreateAppConfig_Negative(t *testing.T) {
	t.Run("returns error when no flags and environment variables are set", func(t *testing.T) {
		cfg, err := CreateAppConfig([]string{})
		assert.Error(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("returns error when an invalid flag is passed", func(t *testing.T) {
		cfg, err := CreateAppConfig([]string{"-invalid-flag-structure"})
		assert.Error(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("returns error when empty environment RUN_ADDRESS is passed", func(t *testing.T) {
		t.Setenv("RUN_ADDRESS", " ")
		t.Setenv("DATABASE_URI", "db_address")
		t.Setenv("ACCRUAL_SYSTEM_ADDRESS", "accrual_system_address")
		cfg, err := CreateAppConfig([]string{})
		assert.ErrorIs(t, err, errAddrEmpty)
		assert.Nil(t, cfg)
	})

	t.Run("returns error when empty environment DATABASE_URI is passed", func(t *testing.T) {
		t.Setenv("RUN_ADDRESS", "localhost:8080")
		t.Setenv("DATABASE_URI", "   ")
		t.Setenv("ACCRUAL_SYSTEM_ADDRESS", "accrual_system_address")
		cfg, err := CreateAppConfig([]string{})
		assert.ErrorIs(t, err, errDSNEmpty)
		assert.Nil(t, cfg)
	})

	t.Run("returns error when empty environment ACCRUAL_SYSTEM_ADDRESS is passed", func(t *testing.T) {
		t.Setenv("RUN_ADDRESS", "localhost:8080")
		t.Setenv("DATABASE_URI", "db_address")
		t.Setenv("ACCRUAL_SYSTEM_ADDRESS", " ")
		cfg, err := CreateAppConfig([]string{})
		assert.ErrorIs(t, err, errAccrualSysAddr)
		assert.Nil(t, cfg)
	})
}

func TestParseFlags(t *testing.T) {
	t.Run("should parse custom flags successfully", func(t *testing.T) {
		args := []string{"-a", "127.0.0.1:4444", "-d", "db_address", "-r", "accrual_system_address"}
		cfg, err := parseFlags(args)

		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, "127.0.0.1:4444", cfg.Addr)
		assert.Equal(t, "db_address", cfg.DatabaseURI)
		assert.Equal(t, "accrual_system_address", cfg.AccrualSysAddr)
	})

	t.Run("should return error on invalid flags", func(t *testing.T) {
		args := []string{"-unsupported-flag"}
		cfg, err := parseFlags(args)

		assert.Error(t, err)
		assert.Nil(t, cfg)
	})
}

func TestParseConfig(t *testing.T) {
	t.Run("should parse valid environment variables", func(t *testing.T) {
		t.Setenv("RUN_ADDRESS", "some-address")
		t.Setenv("DATABASE_URI", "some-database-uri")
		t.Setenv("ACCRUAL_SYSTEM_ADDRESS", "some-accrual-system-address")

		cfg, err := parseEnvConfig()
		require.NotNil(t, cfg)
		require.Nil(t, err)

		assert.Equal(t, *cfg.Addr, "some-address")
		assert.Equal(t, *cfg.URI, "some-database-uri")
		assert.Equal(t, *cfg.AccrualSysAddr, "some-accrual-system-address")
	})

	t.Run("should parse some valid environment variables", func(t *testing.T) {
		t.Setenv("ACCRUAL_SYSTEM_ADDRESS", "some-accrual-system-address")

		cfg, err := parseEnvConfig()
		require.NotNil(t, cfg)
		require.Nil(t, err)

		assert.Nil(t, cfg.Addr)
		assert.Nil(t, cfg.URI)
		assert.Equal(t, *cfg.AccrualSysAddr, "some-accrual-system-address")
	})

	t.Run("should succeed when no env vars are defined (pointers are nil)", func(t *testing.T) {
		t.Setenv("RUN_ADDRESS", "")
		t.Setenv("DATABASE_URI", "")
		t.Setenv("ACCRUAL_SYSTEM_ADDRESS", "")

		cfg, err := parseEnvConfig()
		require.NotNil(t, cfg)
		require.Nil(t, err)

		assert.Nil(t, cfg.Addr)
		assert.Nil(t, cfg.URI)
		assert.Nil(t, cfg.AccrualSysAddr)
	})

	t.Run("should fail when address is empty", func(t *testing.T) {
		t.Setenv("RUN_ADDRESS", "   ")
		t.Setenv("DATABASE_URI", "")
		t.Setenv("ACCRUAL_SYSTEM_ADDRESS", "")

		cfg, err := parseEnvConfig()
		require.Nil(t, cfg)
		require.NotNil(t, err)

		assert.ErrorIs(t, err, errAddrEmpty)
	})

	t.Run("should fail when database URI is empty", func(t *testing.T) {
		t.Setenv("RUN_ADDRESS", "")
		t.Setenv("DATABASE_URI", "    ")
		t.Setenv("ACCRUAL_SYSTEM_ADDRESS", "")

		cfg, err := parseEnvConfig()
		require.Nil(t, cfg)
		require.NotNil(t, err)

		assert.ErrorIs(t, err, errDSNEmpty)
	})

	t.Run("should fail when accrual system address is empty", func(t *testing.T) {
		t.Setenv("RUN_ADDRESS", "")
		t.Setenv("DATABASE_URI", "")
		t.Setenv("ACCRUAL_SYSTEM_ADDRESS", "  ")

		cfg, err := parseEnvConfig()
		require.Nil(t, cfg)
		require.NotNil(t, err)

		assert.ErrorIs(t, err, errAccrualSysAddr)
	})
}

func TestValidateEnvConfig(t *testing.T) {
	testCases := []struct {
		name string
		cfg  envConfig
		want error
	}{
		{
			name: "All variables are set correctly",
			cfg: envConfig{
				Addr:           new("some-correct-addr"),
				URI:            new("some-correct-dsn"),
				AccrualSysAddr: new("some-correct-accrual-system-address"),
			},
			want: nil,
		},
		{
			name: "Address is an empty string",
			cfg: envConfig{
				Addr:           new(""),
				URI:            new("some-correct-dsn"),
				AccrualSysAddr: new("some-correct-accrual-system-address"),
			},
			want: errAddrEmpty,
		},
		{
			name: "DSN is an empty string",
			cfg: envConfig{
				Addr:           new("some-correct-addr"),
				URI:            new(""),
				AccrualSysAddr: new("some-correct-accrual-system-address"),
			},
			want: errDSNEmpty,
		},
		{
			name: "Accrual system address is an empty string",
			cfg: envConfig{
				Addr:           new("some-correct-addr"),
				URI:            new("some-correct-dsn"),
				AccrualSysAddr: new(""),
			},
			want: errAccrualSysAddr,
		},
		{
			name: "Variable as a whitespace-only string",
			cfg: envConfig{
				Addr:           new("    "),
				URI:            new("some-correct-dsn"),
				AccrualSysAddr: new("some-correct-accrual-system-address"),
			},
			want: errAddrEmpty,
		},
		{
			name: "Several empty strings fail on the first one",
			cfg: envConfig{
				Addr:           new(""),
				URI:            new("    "),
				AccrualSysAddr: new("  "),
			},
			want: errAddrEmpty,
		},
	}

	for _, tc := range testCases {
		result := validateEnvConfig(&tc.cfg)

		assert.Equal(t, result, tc.want)
	}
}
