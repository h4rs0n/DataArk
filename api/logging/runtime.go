package logging

import (
	"errors"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Runtime owns the daily writer and restores process-global logging outputs
// when it is closed.
type Runtime struct {
	daily                  *DailyWriter
	previousLogWriter      io.Writer
	previousGinWriter      io.Writer
	previousGinErrorWriter io.Writer
	closeOnce              sync.Once
	closeErr               error
}

// Configure mirrors standard-library and Gin logs to stdout and a retained
// daily file.
func Configure(dir string, retentionDays int) (*Runtime, error) {
	return configure(dir, retentionDays, os.Stdout, time.Now, true)
}

func configure(dir string, retentionDays int, console io.Writer, now func() time.Time, scheduleRotation bool) (*Runtime, error) {
	if console == nil {
		return nil, errors.New("log console writer must not be nil")
	}
	daily, err := newDailyWriter(dir, retentionDays, now, scheduleRotation)
	if err != nil {
		return nil, err
	}
	combined := io.MultiWriter(console, daily)
	runtime := &Runtime{
		daily:                  daily,
		previousLogWriter:      log.Writer(),
		previousGinWriter:      gin.DefaultWriter,
		previousGinErrorWriter: gin.DefaultErrorWriter,
	}
	log.SetOutput(combined)
	gin.DefaultWriter = combined
	gin.DefaultErrorWriter = combined
	return runtime, nil
}

func (runtime *Runtime) Close() error {
	runtime.closeOnce.Do(func() {
		log.SetOutput(runtime.previousLogWriter)
		gin.DefaultWriter = runtime.previousGinWriter
		gin.DefaultErrorWriter = runtime.previousGinErrorWriter
		runtime.closeErr = runtime.daily.Close()
	})
	return runtime.closeErr
}

var _ io.Closer = (*Runtime)(nil)
