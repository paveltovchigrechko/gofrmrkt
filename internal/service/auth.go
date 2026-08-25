package service

import (
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken            = errors.New("token is not valid")
	ErrUnexpectedSigningMethod = errors.New("unexpected signing method")

	errSecretKeyEmpty = errors.New("secret key is empty")
	errTokenTTLNull   = errors.New("token time-to-live is zero")
)

type Claims struct {
	jwt.RegisteredClaims
	UserID int64
}

type AuthService struct {
	secretKey string
	tokenTTL  time.Duration
}

func NewAuthService(secretKey string, tokenTTL time.Duration) (*AuthService, error) {
	if strings.TrimSpace(secretKey) == "" {
		return nil, errSecretKeyEmpty
	}

	if tokenTTL == 0 {
		return nil, errTokenTTLNull
	}

	return &AuthService{
		secretKey: secretKey,
		tokenTTL:  tokenTTL,
	}, nil
}

func (a *AuthService) BuildJWTString(userID int64) (string, error) {
	// Create new token with HS256 signature and Claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(a.tokenTTL)),
		},
		// own claim
		UserID: userID,
	})

	// Create token string
	tokenString, err := token.SignedString([]byte(a.secretKey))
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

func (a *AuthService) GetUserID(tokenString string) (int64, error) {
	claims := &Claims{}
	err := a.validateToken(tokenString, claims)
	if err != nil {
		return -1, err
	}

	return claims.UserID, nil
}

func (a *AuthService) validateToken(tokenString string, claims *Claims) error {
	token, err := jwt.ParseWithClaims(tokenString, claims,
		func(t *jwt.Token) (any, error) {
			if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, ErrUnexpectedSigningMethod
			}
			return []byte(a.secretKey), nil
		})

	if err != nil {
		return err
	}

	if !token.Valid {
		return ErrInvalidToken
	}

	return nil
}
