package logging

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestConfigureMirrorsStandardGinAndRecoveryLogs(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })

	var console bytes.Buffer
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	runtime, err := configure(t.TempDir(), 7, &console, func() time.Time { return now }, false)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runtime.daily.dir, "dataarkapi-2026-08-04.log")

	log.Print("standard fixture")
	router := gin.Default()
	router.GET("/ok", func(context *gin.Context) { context.Status(http.StatusNoContent) })
	router.GET("/panic", func(*gin.Context) { panic("panic fixture") })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ok", nil))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panic", nil))

	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	fileContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"standard fixture", "GET", "/ok", "panic fixture"} {
		if !strings.Contains(console.String(), expected) {
			t.Errorf("console missing %q: %s", expected, console.String())
		}
		if !strings.Contains(string(fileContent), expected) {
			t.Errorf("file missing %q: %s", expected, fileContent)
		}
	}
}
