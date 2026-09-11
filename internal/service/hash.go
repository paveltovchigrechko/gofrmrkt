package service

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

var (
	errHashFail = errors.New("failed to hash password")
)

// HashPassword generates a bcrypt hash of the plain-text password.
func HashPassword(password string) (string, error) {
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", errHashFail
	}
	return string(hashedBytes), nil
}

// CheckPasswordHash compares a plain-text password with the stored hash during login.
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
