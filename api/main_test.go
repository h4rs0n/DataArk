package main

import (
	"DataArk/config"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestMainRunsStartupSequence(t *testing.T) {
	oldParseFlags := parseFlags
	oldStartWeb := startWeb
	oldConfigureLogging := configureLogging
	oldDebug := config.DEBUG
	oldLogDir := config.LOGDIR
	oldLogRetentionDays := config.LOGRETENTIONDAYS
	t.Cleanup(func() {
		parseFlags = oldParseFlags
		startWeb = oldStartWeb
		configureLogging = oldConfigureLogging
		config.DEBUG = oldDebug
		config.LOGDIR = oldLogDir
		config.LOGRETENTIONDAYS = oldLogRetentionDays
	})

	var calls []string
	parseFlags = func() {
		calls = append(calls, "parse")
		config.DEBUG = true
		config.LOGDIR = "/fixture/logs"
		config.LOGRETENTIONDAYS = 9
	}
	configureLogging = func(dir string, retentionDays int) (io.Closer, error) {
		if dir != "/fixture/logs" || retentionDays != 9 {
			t.Fatalf("logging config = %q/%d", dir, retentionDays)
		}
		calls = append(calls, "logging")
		return closerFunc(func() error {
			calls = append(calls, "close")
			return nil
		}), nil
	}
	startWeb = func(debug bool) {
		if !debug {
			t.Fatal("startWeb received debug=false, want true")
		}
		calls = append(calls, "start")
	}

	output := captureStdout(t, func() {
		if err := run(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "_____") {
		t.Fatalf("banner output = %q, want ASCII banner", output)
	}
	if strings.Join(calls, ",") != "parse,logging,start,close" {
		t.Fatalf("calls = %#v, want parse, logging, start, close", calls)
	}
}

func TestRunStopsWhenLoggingInitializationFails(t *testing.T) {
	oldParseFlags := parseFlags
	oldStartWeb := startWeb
	oldConfigureLogging := configureLogging
	t.Cleanup(func() {
		parseFlags = oldParseFlags
		startWeb = oldStartWeb
		configureLogging = oldConfigureLogging
	})

	parseFlags = func() {}
	configureLogging = func(string, int) (io.Closer, error) {
		return nil, errors.New("permission denied")
	}
	startWeb = func(bool) { t.Fatal("web service should not start") }
	if err := run(); err == nil || !strings.Contains(err.Error(), "initialize logging") {
		t.Fatalf("run error = %v", err)
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

type closerFunc func() error

func (closer closerFunc) Close() error {
	return closer()
}
