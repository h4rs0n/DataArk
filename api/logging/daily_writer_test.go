package logging

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDailyWriterRotatesAndRetainsCalendarDays(t *testing.T) {
	dir := t.TempDir()
	location := time.FixedZone("fixture", 8*60*60)
	now := time.Date(2026, time.August, 4, 23, 59, 0, 0, location)
	clock := func() time.Time { return now }

	fixtures := map[string]bool{
		"dataarkapi-2026-07-28.log":        false,
		"dataarkapi-2026-07-29.log":        true,
		"dataarkapi-2026-08-03.log":        true,
		"dataarkapi-2026-08-09.log":        true,
		"dataarkapi-not-a-date.log":        true,
		"dataarkapi-2026-07-20.log.backup": true,
		"operator-notes.txt":               true,
	}
	for name := range fixtures {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	writer, err := newDailyWriter(dir, 7, clock, false)
	if err != nil {
		t.Fatalf("newDailyWriter returned error: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	for name, shouldRemain := range fixtures {
		_, statErr := os.Stat(filepath.Join(dir, name))
		if shouldRemain && statErr != nil {
			t.Errorf("expected %s to remain: %v", name, statErr)
		}
		if !shouldRemain && !os.IsNotExist(statErr) {
			t.Errorf("expected %s to be removed, stat error=%v", name, statErr)
		}
	}

	if _, err := writer.Write([]byte("before midnight\n")); err != nil {
		t.Fatal(err)
	}
	now = time.Date(2026, time.August, 5, 0, 1, 0, 0, location)
	if _, err := writer.Write([]byte("after midnight\n")); err != nil {
		t.Fatal(err)
	}

	assertFileContains(t, filepath.Join(dir, "dataarkapi-2026-08-04.log"), "before midnight")
	assertFileContains(t, filepath.Join(dir, "dataarkapi-2026-08-05.log"), "after midnight")
	if _, err := os.Stat(filepath.Join(dir, "dataarkapi-2026-07-29.log")); !os.IsNotExist(err) {
		t.Fatalf("July 29 should expire after rotating to August 5, stat error=%v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "dataarkapi-2026-08-05.log"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o027 != 0 {
		t.Fatalf("log file permissions = %o, want no group write or other access", info.Mode().Perm())
	}
}

func TestDailyWriterAppendsAndSerializesConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(dir, "dataarkapi-2026-08-04.log")
	if err := os.WriteFile(path, []byte("existing\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	writer, err := newDailyWriter(dir, 7, func() time.Time { return now }, false)
	if err != nil {
		t.Fatal(err)
	}

	const writers = 24
	var group sync.WaitGroup
	for index := 0; index < writers; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			if _, writeErr := fmt.Fprintf(writer, "line-%02d\n", index); writeErr != nil {
				t.Errorf("concurrent write: %v", writeErr)
			}
		}(index)
	}
	group.Wait()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("second close should be safe: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(content, []byte("existing\n")) {
		t.Fatalf("existing content was truncated: %q", content)
	}
	if got := strings.Count(string(content), "line-"); got != writers {
		t.Fatalf("concurrent line count = %d, want %d", got, writers)
	}
	if _, err := writer.Write([]byte("closed")); !os.IsNotExist(err) && err != os.ErrClosed {
		t.Fatalf("write after close error = %v, want os.ErrClosed", err)
	}
}

func TestDailyWriterRejectsInvalidDestinations(t *testing.T) {
	if _, err := newDailyWriter("", 7, time.Now, false); err == nil {
		t.Fatal("empty directory should fail")
	}
	if _, err := newDailyWriter(t.TempDir(), 0, time.Now, false); err == nil {
		t.Fatal("zero retention should fail")
	}
	filePath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(filePath, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newDailyWriter(filepath.Join(filePath, "logs"), 7, time.Now, false); err == nil {
		t.Fatal("directory beneath a regular file should fail")
	}
}

func assertFileContains(t *testing.T, path string, expected string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), expected) {
		t.Fatalf("%s = %q, want %q", path, content, expected)
	}
}
