package main

import (
	"DataArk/api"
	"DataArk/config"
	dataarkflag "DataArk/flag"
	dataarklogging "DataArk/logging"
	"errors"
	"fmt"
	"io"
	"os"
)

var (
	parseFlags       = dataarkflag.ParseFlag
	startWeb         = api.WebStarter
	configureLogging = func(dir string, retentionDays int) (io.Closer, error) {
		return dataarklogging.Configure(dir, retentionDays)
	}
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "DataArk startup failed: %v\n", err)
		os.Exit(1)
	}
}

func run() (runErr error) {
	display_banner()
	parseFlags()
	if err := config.ValidateRequiredLLM(); err != nil {
		return fmt.Errorf("llm configuration: %w", err)
	}
	logRuntime, err := configureLogging(config.LOGDIR, config.LOGRETENTIONDAYS)
	if err != nil {
		return fmt.Errorf("initialize logging: %w", err)
	}
	defer func() {
		runErr = errors.Join(runErr, logRuntime.Close())
	}()
	startWeb(config.DEBUG)
	return nil
}

func display_banner() {
	fmt.Println("  _____     _             _    ____  _  __")
	fmt.Println(" | ____|___| |__   ___   / \\  |  _ \\| |/ /")
	fmt.Println(" |  _| / __| '_ \\ / _ \\ / _ \\ | |_) | ' / ")
	fmt.Println(" | |__| (__| | | | (_) / ___ \\|  _ <| . \\ ")
	fmt.Println(" |_____\\___|_| |_|\\___/_/   \\_\\_| \\_\\_|\\_\\")
	fmt.Println("                                          ")
}
