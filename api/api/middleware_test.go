package api

import (
	"DataArk/common"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareBranches(t *testing.T) {
	t.Run("missing header", func(t *testing.T) {
		response := performMiddlewareRequest(AuthMiddleware(), "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", response.Code)
		}
	})

	t.Run("invalid header", func(t *testing.T) {
		withAuthFakes(t, func(header string) (string, error) {
			return "", errors.New("bad header")
		}, nil, nil)
		response := performMiddlewareRequest(AuthMiddleware(), "bad")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", response.Code)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		withAuthFakes(t,
			func(header string) (string, error) { return "token", nil },
			func(token string) (*common.Claims, error) { return nil, errors.New("bad token") },
			nil,
		)
		response := performMiddlewareRequest(AuthMiddleware(), "Bearer token")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", response.Code)
		}
	})

	t.Run("missing user", func(t *testing.T) {
		withAuthFakes(t,
			func(header string) (string, error) { return "token", nil },
			func(token string) (*common.Claims, error) { return &common.Claims{UserID: 9, Username: "missing"}, nil },
			func(id uint) (*common.User, error) { return nil, errors.New("not found") },
		)
		response := performMiddlewareRequest(AuthMiddleware(), "Bearer token")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", response.Code)
		}
	})

	t.Run("success", func(t *testing.T) {
		withAuthFakes(t,
			func(header string) (string, error) { return "token", nil },
			func(token string) (*common.Claims, error) { return &common.Claims{UserID: 1, Username: "alice"}, nil },
			func(id uint) (*common.User, error) { return &common.User{ID: id, Username: "alice"}, nil },
		)
		response := performMiddlewareRequest(AuthMiddleware(), "Bearer token")
		if response.Code != http.StatusOK || response.Body.String() != "alice" {
			t.Fatalf("status=%d body=%q, want 200 alice", response.Code, response.Body.String())
		}
	})
}

func TestOptionalAuthMiddlewareBranches(t *testing.T) {
	t.Run("missing header continues", func(t *testing.T) {
		response := performMiddlewareRequest(OptionalAuthMiddleware(), "")
		if response.Code != http.StatusOK || response.Body.String() != "anonymous" {
			t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
		}
	})

	t.Run("bad header continues", func(t *testing.T) {
		withAuthFakes(t, func(header string) (string, error) { return "", errors.New("bad") }, nil, nil)
		response := performMiddlewareRequest(OptionalAuthMiddleware(), "bad")
		if response.Code != http.StatusOK || response.Body.String() != "anonymous" {
			t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
		}
	})

	t.Run("bad token continues", func(t *testing.T) {
		withAuthFakes(t,
			func(header string) (string, error) { return "token", nil },
			func(token string) (*common.Claims, error) { return nil, errors.New("bad token") },
			nil,
		)
		response := performMiddlewareRequest(OptionalAuthMiddleware(), "Bearer token")
		if response.Code != http.StatusOK || response.Body.String() != "anonymous" {
			t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
		}
	})

	t.Run("missing user continues", func(t *testing.T) {
		withAuthFakes(t,
			func(header string) (string, error) { return "token", nil },
			func(token string) (*common.Claims, error) { return &common.Claims{UserID: 2}, nil },
			func(id uint) (*common.User, error) { return nil, errors.New("missing") },
		)
		response := performMiddlewareRequest(OptionalAuthMiddleware(), "Bearer token")
		if response.Code != http.StatusOK || response.Body.String() != "anonymous" {
			t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
		}
	})

	t.Run("success sets context", func(t *testing.T) {
		withAuthFakes(t,
			func(header string) (string, error) { return "token", nil },
			func(token string) (*common.Claims, error) { return &common.Claims{UserID: 3, Username: "carol"}, nil },
			func(id uint) (*common.User, error) { return &common.User{ID: id, Username: "carol"}, nil },
		)
		response := performMiddlewareRequest(OptionalAuthMiddleware(), "Bearer token")
		if response.Code != http.StatusOK || response.Body.String() != "carol" {
			t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
		}
	})
}

func TestContextAuthHelpers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	if IsAuthenticated(context) {
		t.Fatal("empty context should not be authenticated")
	}
	if _, ok := GetCurrentUser(context); ok {
		t.Fatal("empty context should not return user")
	}
	if _, ok := GetCurrentUserID(context); ok {
		t.Fatal("empty context should not return user id")
	}
	if _, ok := GetCurrentUsername(context); ok {
		t.Fatal("empty context should not return username")
	}

	user := &common.User{ID: 5, Username: "dana"}
	context.Set("user", user)
	context.Set("user_id", user.ID)
	context.Set("username", user.Username)
	if got, ok := GetCurrentUser(context); !ok || got != user {
		t.Fatalf("GetCurrentUser = %#v %v", got, ok)
	}
	if got, ok := GetCurrentUserID(context); !ok || got != 5 {
		t.Fatalf("GetCurrentUserID = %d %v", got, ok)
	}
	if got, ok := GetCurrentUsername(context); !ok || got != "dana" {
		t.Fatalf("GetCurrentUsername = %q %v", got, ok)
	}
	if !IsAuthenticated(context) {
		t.Fatal("context should be authenticated")
	}
	if got, ok := RequireAuth(context); !ok || got != user {
		t.Fatalf("RequireAuth = %#v %v", got, ok)
	}

	context2, _ := gin.CreateTestContext(httptest.NewRecorder())
	if _, ok := RequireAuth(context2); ok {
		t.Fatal("RequireAuth should reject missing user")
	}
}

func performMiddlewareRequest(middleware gin.HandlerFunc, authHeader string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware)
	router.GET("/", func(c *gin.Context) {
		if username, ok := GetCurrentUsername(c); ok {
			c.String(http.StatusOK, username)
			return
		}
		c.String(http.StatusOK, "anonymous")
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	if authHeader != "" {
		request.Header.Set("Authorization", authHeader)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func withAuthFakes(
	t *testing.T,
	extract func(string) (string, error),
	validate func(string) (*common.Claims, error),
	getUser func(uint) (*common.User, error),
) {
	t.Helper()
	oldExtract := extractTokenFromHeader
	oldValidate := validateToken
	oldGetUser := getUserByID
	t.Cleanup(func() {
		extractTokenFromHeader = oldExtract
		validateToken = oldValidate
		getUserByID = oldGetUser
	})
	if extract != nil {
		extractTokenFromHeader = extract
	}
	if validate != nil {
		validateToken = validate
	}
	if getUser != nil {
		getUserByID = getUser
	}
}
