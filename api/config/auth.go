package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// JWTSecret is independent of all external service credentials.
var JWTSecret string

// InitializeAuth runs after configuration is loaded and before serving requests.
// An empty secret gets one random key for this process, shared by all tokens.
func InitializeAuth() error {
	if strings.TrimSpace(JWTSecret) == "" {
		var key [32]byte
		if _, err := rand.Read(key[:]); err != nil {
			return fmt.Errorf("generate JWT signing secret: %w", err)
		}
		JWTSecret = hex.EncodeToString(key[:])
	}
	return ValidateAuth()
}

func ValidateAuth() error {
	if strings.TrimSpace(JWTSecret) == "" || len(JWTSecret) < 32 {
		return fmt.Errorf("-jwt-secret (or JWT_SECRET) must contain at least 32 bytes")
	}
	return nil
}
