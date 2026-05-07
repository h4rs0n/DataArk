package main

import (
	"DataArk/common"
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestMainRunsStartupSequence(t *testing.T) {
	oldParseFlags := parseFlags
	oldStartWeb := startWeb
	oldDebug := common.DEBUG
	t.Cleanup(func() {
		parseFlags = oldParseFlags
		startWeb = oldStartWeb
		common.DEBUG = oldDebug
	})

	var calls []string
	parseFlags = func() {
		calls = append(calls, "parse")
		common.DEBUG = true
	}
	startWeb = func(debug bool) {
		if !debug {
			t.Fatal("startWeb received debug=false, want true")
		}
		calls = append(calls, "start")
	}

	output := captureStdout(t, main)

	if !strings.Contains(output, "_____") {
		t.Fatalf("banner output = %q, want ASCII banner", output)
	}
	if strings.Join(calls, ",") != "parse,start" {
		t.Fatalf("calls = %#v, want parse then start", calls)
	}
}

func TestDisplayBannerWritesBanner(t *testing.T) {
	output := captureStdout(t, display_banner)
	if !strings.Contains(output, "____") || !strings.Contains(output, "| ____|") {
		t.Fatalf("unexpected banner output: %q", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = oldStdout
	})

	fn()
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close writer: %v", err)
	}

	var buffer bytes.Buffer
	if _, err := io.Copy(&buffer, reader); err != nil {
		t.Fatalf("failed to read stdout: %v", err)
	}
	return buffer.String()
}
