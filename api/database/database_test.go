package database

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestNewGORMLoggerUsesConfiguredWriterWithoutColor(t *testing.T) {
	var output bytes.Buffer
	newGORMLogger(&output).Error(context.Background(), "database fixture")
	if !strings.Contains(output.String(), "database fixture") {
		t.Fatalf("GORM output = %q", output.String())
	}
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("GORM file output contains terminal color: %q", output.String())
	}
}
