package config

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestInitializeAuthGeneratesStableRandomSecretWhenEmpty(t *testing.T) {
	old := JWTSecret
	t.Cleanup(func() { JWTSecret = old })
	var previous string
	for _, empty := range []string{"", "   "} {
		JWTSecret = empty
		if err := InitializeAuth(); err != nil {
			t.Fatal(err)
		}
		key, err := hex.DecodeString(JWTSecret)
		if err != nil || len(key) != 32 {
			t.Fatal("generated secret must encode 32 random bytes")
		}
		generated := JWTSecret
		if generated == previous {
			t.Fatal("new initialization reused the previous random secret")
		}
		if err := InitializeAuth(); err != nil {
			t.Fatal(err)
		}
		if JWTSecret != generated {
			t.Fatal("reinitialization rotated the active signing secret")
		}
		previous = generated
	}
}

func TestInitializeAuthPreservesConfiguredSecretAndRejectsShortSecret(t *testing.T) {
	old := JWTSecret
	t.Cleanup(func() { JWTSecret = old })
	configured := strings.Repeat("configured-key-", 3)
	JWTSecret = configured
	if err := InitializeAuth(); err != nil {
		t.Fatal(err)
	}
	if JWTSecret != configured {
		t.Fatal("replaced an explicitly configured secret")
	}
	JWTSecret = "short"
	if err := InitializeAuth(); err == nil {
		t.Fatal("accepted an explicitly configured short secret")
	}
}
