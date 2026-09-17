package service

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAuthService(t *testing.T) {
	t.Run("successfully creates AuthService with valid parameters", func(t *testing.T) {
		authService, err := NewAuthService("valid-secret", 1*time.Hour)
		require.NoError(t, err)
		assert.NotNil(t, authService)
	})

	t.Run("returns error when secret key is empty", func(t *testing.T) {
		authService, err := NewAuthService("", 1*time.Hour)
		assert.Nil(t, authService)
		assert.ErrorIs(t, err, errSecretKeyEmpty)
	})

	t.Run("returns error when secret key is whitespace only", func(t *testing.T) {
		authService, err := NewAuthService("   ", 1*time.Hour)
		assert.Nil(t, authService)
		assert.ErrorIs(t, err, errSecretKeyEmpty)
	})

	t.Run("returns error when token TTL is zero", func(t *testing.T) {
		authService, err := NewAuthService("valid-secret", 0)
		assert.Nil(t, authService)
		assert.ErrorIs(t, err, errTokenTTLNull)
	})
}

func TestAuthService_JWT(t *testing.T) {
	secretKey := "test-secret-key"
	tokenTTL := 1 * time.Hour

	authService, err := NewAuthService(secretKey, tokenTTL)
	require.NoError(t, err)

	t.Run("successfully builds and retrieves user ID from valid token", func(t *testing.T) {
		var expectedUserID int64 = 42

		tokenString, err := authService.BuildJWTString(expectedUserID)
		require.NoError(t, err)
		assert.NotEmpty(t, tokenString)

		userID, err := authService.GetUserID(tokenString)
		assert.NoError(t, err)
		assert.Equal(t, expectedUserID, userID)
	})

	t.Run("fails when token is signed with a different secret key", func(t *testing.T) {
		differentAuthService, err := NewAuthService("wrong-secret-key", tokenTTL)
		require.NoError(t, err)

		tokenString, err := authService.BuildJWTString(100)
		require.NoError(t, err)

		userID, err := differentAuthService.GetUserID(tokenString)
		assert.Error(t, err)
		assert.Equal(t, int64(-1), userID)
	})

	t.Run("fails when token is expired", func(t *testing.T) {
		// Construct expired token using negative TTL bypass via custom service instance
		expiredAuthService := &AuthService{
			secretKey: secretKey,
			tokenTTL:  -1 * time.Minute,
		}

		tokenString, err := expiredAuthService.BuildJWTString(99)
		require.NoError(t, err)

		userID, err := authService.GetUserID(tokenString)
		assert.Error(t, err)
		assert.Equal(t, int64(-1), userID)
	})

	t.Run("fails for malformed or tampered token string", func(t *testing.T) {
		userID, err := authService.GetUserID("invalid.jwt.string")
		assert.Error(t, err)
		assert.Equal(t, int64(-1), userID)
	})

	t.Run("fails when token uses unexpected signing method", func(t *testing.T) {
		claims := Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenTTL)),
			},
			UserID: 77,
		}
		token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
		unsecuredTokenString, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
		require.NoError(t, err)

		userID, err := authService.GetUserID(unsecuredTokenString)
		assert.ErrorIs(t, err, ErrUnexpectedSigningMethod)
		assert.Equal(t, int64(-1), userID)
	})
}
