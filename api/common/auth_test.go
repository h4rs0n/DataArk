package common

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateValidateAndRevokeToken(t *testing.T) {
	oldExpiration := tokenExpiration
	t.Cleanup(func() {
		tokenExpiration = oldExpiration
	})
	SetTokenExpiration(time.Hour)

	user := &User{ID: 42, Username: "alice"}
	tokenResponse, err := GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if tokenResponse.Token == "" || tokenResponse.User != user {
		t.Fatalf("unexpected token response: %#v", tokenResponse)
	}

	claims, err := ValidateToken(tokenResponse.Token)
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if claims.UserID != 42 || claims.Username != "alice" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
	if IsTokenExpired(tokenResponse.Token) {
		t.Fatal("fresh token should not be expired")
	}
	remaining, err := GetTokenRemainingTime(tokenResponse.Token)
	if err != nil {
		t.Fatalf("GetTokenRemainingTime returned error: %v", err)
	}
	if remaining <= 0 {
		t.Fatalf("remaining = %v, want positive", remaining)
	}
	if err := RevokeToken(tokenResponse.Token); err != nil {
		t.Fatalf("RevokeToken returned error: %v", err)
	}
}

func TestTokenErrorBranches(t *testing.T) {
	if _, err := GenerateToken(nil); err == nil {
		t.Fatal("GenerateToken(nil) should return error")
	}
	if _, err := ValidateToken(""); err == nil {
		t.Fatal("ValidateToken empty should return error")
	}
	if _, err := ValidateToken("not-a-token"); err == nil || !strings.Contains(err.Error(), "failed to parse token") {
		t.Fatalf("ValidateToken invalid err = %v", err)
	}
	if !IsTokenExpired("not-a-token") {
		t.Fatal("invalid token should be treated as expired")
	}
	if _, err := GetTokenRemainingTime("not-a-token"); err == nil {
		t.Fatal("GetTokenRemainingTime invalid should return error")
	}
	if err := RevokeToken("not-a-token"); err == nil || !strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("RevokeToken invalid err = %v", err)
	}
}

func TestExpiredTokenBranches(t *testing.T) {
	oldExpiration := tokenExpiration
	t.Cleanup(func() {
		tokenExpiration = oldExpiration
	})
	SetTokenExpiration(-time.Minute)

	tokenResponse, err := GenerateToken(&User{ID: 7, Username: "expired"})
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if !IsTokenExpired(tokenResponse.Token) {
		t.Fatal("expired token should be expired")
	}
	if _, err := GetTokenRemainingTime(tokenResponse.Token); err == nil {
		t.Fatal("GetTokenRemainingTime expired should return error")
	}
}

func TestExtractTokenFromHeader(t *testing.T) {
	token, err := ExtractTokenFromHeader("Bearer abc123")
	if err != nil {
		t.Fatalf("ExtractTokenFromHeader returned error: %v", err)
	}
	if token != "abc123" {
		t.Fatalf("token = %q, want abc123", token)
	}

	if _, err := ExtractTokenFromHeader(""); err == nil {
		t.Fatal("empty header should return error")
	}
	if _, err := ExtractTokenFromHeader("Basic abc123"); err == nil {
		t.Fatal("non-bearer header should return error")
	}
	if _, err := ExtractTokenFromHeader("Bearer "); err == nil {
		t.Fatal("empty bearer token should return error")
	}
}

func TestTokenDatabaseHelpers(t *testing.T) {
	setupSQLiteDB(t)
	oldExpiration := tokenExpiration
	t.Cleanup(func() {
		tokenExpiration = oldExpiration
	})

	SetTokenExpiration(10 * time.Minute)
	registered, err := RegisterWithToken("token-user", "secret123")
	if err != nil {
		t.Fatalf("RegisterWithToken returned error: %v", err)
	}
	if registered.Token == "" || registered.User.Username != "token-user" {
		t.Fatalf("registered = %#v", registered)
	}

	loggedIn, err := LoginWithToken("token-user", "secret123")
	if err != nil {
		t.Fatalf("LoginWithToken returned error: %v", err)
	}
	if loggedIn.Token == "" {
		t.Fatal("LoginWithToken should return token")
	}

	user, err := GetUserFromToken(loggedIn.Token)
	if err != nil {
		t.Fatalf("GetUserFromToken returned error: %v", err)
	}
	if user.Username != "token-user" {
		t.Fatalf("user = %#v", user)
	}

	refreshed, err := RefreshToken(loggedIn.Token)
	if err != nil {
		t.Fatalf("RefreshToken returned error: %v", err)
	}
	if refreshed.Token == "" {
		t.Fatal("RefreshToken should return a new token")
	}
	if _, err := RefreshToken("bad-token"); err == nil {
		t.Fatal("RefreshToken invalid token should fail")
	}

	SetTokenExpiration(2 * time.Hour)
	longLived, err := GenerateToken(&User{ID: user.ID, Username: user.Username})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RefreshToken(longLived.Token); err == nil || !strings.Contains(err.Error(), "refresh not needed") {
		t.Fatalf("RefreshToken long-lived err = %v", err)
	}
}
