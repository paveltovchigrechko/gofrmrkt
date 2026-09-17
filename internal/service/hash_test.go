package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashPassword(t *testing.T) {
	t.Run("successfully hashes password", func(t *testing.T) {
		password := "secret123"
		hash, err := HashPassword(password)

		assert.NoError(t, err)
		assert.NotEmpty(t, hash)
		assert.NotEqual(t, password, hash)
	})

	t.Run("generates unique hashes for identical passwords", func(t *testing.T) {
		password := "myPassword"
		hash1, err1 := HashPassword(password)
		hash2, err2 := HashPassword(password)

		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.NotEqual(t, hash1, hash2, "Bcrypt salt should ensure distinct hashes")
	})
}

func TestCheckPasswordHash(t *testing.T) {
	password := "correctPassword"
	hash, err := HashPassword(password)
	assert.NoError(t, err)

	t.Run("returns true for correct password", func(t *testing.T) {
		match := CheckPasswordHash(password, hash)
		assert.True(t, match)
	})

	t.Run("returns false for incorrect password", func(t *testing.T) {
		match := CheckPasswordHash("wrongPassword", hash)
		assert.False(t, match)
	})

	t.Run("returns false for malformed hash", func(t *testing.T) {
		match := CheckPasswordHash(password, "invalid_hash_format")
		assert.False(t, match)
	})
}
