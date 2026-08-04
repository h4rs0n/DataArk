package logging

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	logFilePrefix = "dataarkapi-"
	logFileSuffix = ".log"
	logDateLayout = "2006-01-02"
)

// DailyWriter appends logs to one file per server-local calendar date and
// removes matching files outside the configured retention window.
type DailyWriter struct {
	mu            sync.Mutex
	dir           string
	retentionDays int
	now           func() time.Time
	currentDate   string
	file          *os.File
	stop          chan struct{}
	done          chan struct{}
	closeOnce     sync.Once
	closeErr      error
}

// NewDailyWriter prepares a daily log writer and starts its midnight rotation
// loop. retentionDays includes the current local calendar date.
func NewDailyWriter(dir string, retentionDays int) (*DailyWriter, error) {
	return newDailyWriter(dir, retentionDays, time.Now, true)
}

func newDailyWriter(dir string, retentionDays int, now func() time.Time, scheduleRotation bool) (*DailyWriter, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("log directory must not be empty")
	}
	if retentionDays < 1 {
		return nil, errors.New("log retention days must be at least 1")
	}
	if now == nil {
		return nil, errors.New("log clock must not be nil")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create log directory %q: %w", dir, err)
	}

	writer := &DailyWriter{
		dir:           dir,
		retentionDays: retentionDays,
		now:           now,
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
	writer.mu.Lock()
	err := writer.rotateLocked(now())
	writer.mu.Unlock()
	if err != nil {
		if writer.file != nil {
			_ = writer.file.Close()
		}
		return nil, err
	}

	if scheduleRotation {
		go writer.rotationLoop()
	} else {
		close(writer.done)
	}
	return writer, nil
}

func (writer *DailyWriter) Write(payload []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	now := writer.now()
	if writer.file == nil {
		return 0, os.ErrClosed
	}
	if now.Format(logDateLayout) != writer.currentDate {
		if err := writer.rotateLocked(now); err != nil {
			return 0, err
		}
	}
	return writer.file.Write(payload)
}

func (writer *DailyWriter) Close() error {
	writer.closeOnce.Do(func() {
		close(writer.stop)
		<-writer.done

		writer.mu.Lock()
		defer writer.mu.Unlock()
		if writer.file != nil {
			writer.closeErr = writer.file.Close()
			writer.file = nil
		}
	})
	return writer.closeErr
}

func (writer *DailyWriter) rotationLoop() {
	defer close(writer.done)
	for {
		now := writer.now()
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		wait := time.Until(nextMidnight)
		if wait <= 0 {
			wait = time.Minute
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
			writer.mu.Lock()
			err := writer.rotateLocked(writer.now())
			writer.mu.Unlock()
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "dataark log rotation failed: %v\n", err)
			}
		case <-writer.stop:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}

func (writer *DailyWriter) rotateLocked(now time.Time) error {
	date := now.Format(logDateLayout)
	if writer.file == nil || writer.currentDate != date {
		path := filepath.Join(writer.dir, logFilePrefix+date+logFileSuffix)
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
		if err != nil {
			return fmt.Errorf("open daily log file %q: %w", path, err)
		}
		previous := writer.file
		writer.file = file
		writer.currentDate = date
		if previous != nil {
			if err := previous.Close(); err != nil {
				return fmt.Errorf("close previous daily log file: %w", err)
			}
		}
	}
	if err := writer.cleanupLocked(now); err != nil {
		return err
	}
	return nil
}

func (writer *DailyWriter) cleanupLocked(now time.Time) error {
	entries, err := os.ReadDir(writer.dir)
	if err != nil {
		return fmt.Errorf("read log directory %q: %w", writer.dir, err)
	}
	localNow := now.In(now.Location())
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, localNow.Location())
	cutoff := today.AddDate(0, 0, -(writer.retentionDays - 1))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		date, matches := parseLogDate(entry.Name(), localNow.Location())
		if !matches || !date.Before(cutoff) {
			continue
		}
		path := filepath.Join(writer.dir, entry.Name())
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove expired log file %q: %w", path, err)
		}
	}
	return nil
}

func parseLogDate(name string, location *time.Location) (time.Time, bool) {
	if !strings.HasPrefix(name, logFilePrefix) || !strings.HasSuffix(name, logFileSuffix) {
		return time.Time{}, false
	}
	dateText := strings.TrimSuffix(strings.TrimPrefix(name, logFilePrefix), logFileSuffix)
	if len(dateText) != len(logDateLayout) {
		return time.Time{}, false
	}
	date, err := time.ParseInLocation(logDateLayout, dateText, location)
	if err != nil || date.Format(logDateLayout) != dateText {
		return time.Time{}, false
	}
	return date, true
}

var _ io.WriteCloser = (*DailyWriter)(nil)
