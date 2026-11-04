package search

import (
	"DataArk/config"
	"DataArk/material"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type syncCheckServer struct {
	mu           sync.Mutex
	generation   string
	markerStatus int
	writeMissing bool
	requests     []string
}

func (s *syncCheckServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "/documents/"+generationMarker):
		if s.markerStatus != 0 {
			w.WriteHeader(s.markerStatus)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "index_not_found", "message": "fixture failure"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": generationMarker, "generation": s.generation})
	case strings.HasSuffix(r.URL.Path, "/documents/delete-batch"):
		if s.writeMissing {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"code":"index_not_found","message":"missing index"}`))
			return
		}
		w.WriteHeader(202)
		_, _ = w.Write([]byte(`{"taskUid":1,"status":"enqueued"}`))
	case r.URL.Path == "/tasks/1":
		_, _ = w.Write([]byte(`{"uid":1,"status":"succeeded"}`))
	default:
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"message":"unexpected request"}`))
	}
}

func (s *syncCheckServer) markerRequests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, r := range s.requests {
		if strings.HasSuffix(r, "/documents/"+generationMarker) {
			count++
		}
	}
	return count
}

func newSyncCheckFixture(t *testing.T) (*gorm.DB, *syncCheckServer, *syncRunner, *time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&SyncState{}, &syncPending{}, &material.Material{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&SyncState{ID: 1, Phase: "ready", Generation: "test-generation"}).Error; err != nil {
		t.Fatal(err)
	}
	remote := &syncCheckServer{generation: "test-generation"}
	server := httptest.NewServer(remote)
	t.Cleanup(server.Close)
	oldHost, oldKey := config.MEILIHOST, config.MEILIAPIKey
	config.MEILIHOST, config.MEILIAPIKey = server.URL, ""
	t.Cleanup(func() { config.MEILIHOST, config.MEILIAPIKey = oldHost, oldKey })
	now := time.Now()
	runner := &syncRunner{now: func() time.Time { return now }}
	return db, remote, runner, &now
}

func TestSyncIntegrityChecksAreThrottledAndRestartImmediately(t *testing.T) {
	db, remote, runner, now := newSyncCheckFixture(t)
	tick := func() {
		t.Helper()
		if _, err := runner.step(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	tick()
	if remote.markerRequests() != 1 {
		t.Fatal("startup must check once")
	}
	for i := 0; i < 149; i++ {
		*now = now.Add(2 * time.Second)
		tick()
	}
	if remote.markerRequests() != 1 {
		t.Fatal("idle database polls contacted Meilisearch")
	}
	*now = now.Add(2 * time.Second)
	tick()
	if remote.markerRequests() != 2 {
		t.Fatal("five-minute check missing")
	}
	runner = &syncRunner{now: func() time.Time { return *now }}
	tick()
	if remote.markerRequests() != 3 {
		t.Fatal("restart must check immediately")
	}
	remote.mu.Lock()
	remote.generation = "replacement"
	remote.mu.Unlock()
	if err := saveState(db, map[string]interface{}{"generation": "replacement"}); err != nil {
		t.Fatal(err)
	}
	tick()
	if remote.markerRequests() != 4 {
		t.Fatal("new generation must invalidate the schedule")
	}
}

func TestSyncPendingWorkDoesNotWaitForIntegrityTimer(t *testing.T) {
	db, remote, runner, now := newSyncCheckFixture(t)
	ctx := context.Background()
	if _, err := runner.step(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []bool{false, true} {
		*now = now.Add(2 * time.Second)
		if err := db.Create(&syncPending{MaterialID: 42, Revision: 1, NextAttemptAt: time.Now().Add(-time.Second)}).Error; err != nil {
			t.Fatal(err)
		}
		remote.mu.Lock()
		remote.writeMissing = missing
		remote.mu.Unlock()
		worked, err := runner.step(ctx, db)
		if err != nil || !worked {
			t.Fatalf("work=%v err=%v", worked, err)
		}
		var pending int64
		db.Model(&syncPending{}).Count(&pending)
		var state SyncState
		db.First(&state, 1)
		if !missing && (pending != 0 || state.LastSuccessAt == nil) {
			t.Fatal("pending work was not acknowledged")
		}
		if missing && (pending != 1 || state.Phase != "prepare") {
			t.Fatal("missing index did not preserve work and start recovery")
		}
	}
	if remote.markerRequests() != 1 {
		t.Fatal("writes caused extra integrity checks")
	}
}

func TestSyncPeriodicCheckDetectsIndexLossAndRetriesTransientErrors(t *testing.T) {
	for _, mode := range []string{"missing", "replaced", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			db, remote, runner, now := newSyncCheckFixture(t)
			ctx := context.Background()
			if _, err := runner.step(ctx, db); err != nil {
				t.Fatal(err)
			}
			*now = now.Add(5 * time.Minute)
			remote.mu.Lock()
			switch mode {
			case "missing":
				remote.markerStatus = 404
			case "replaced":
				remote.generation = "wrong-generation"
			case "unavailable":
				remote.markerStatus = 503
			}
			remote.mu.Unlock()
			_, err := runner.step(ctx, db)
			var state SyncState
			if err := db.First(&state, 1).Error; err != nil {
				t.Fatal(err)
			}
			if mode != "unavailable" {
				if err != nil || state.Phase != "prepare" {
					t.Fatalf("state=%s err=%v", state.Phase, err)
				}
				return
			}
			if err == nil || state.Phase != "ready" {
				t.Fatalf("transient failure should retain ready state: %s %v", state.Phase, err)
			}
			// The SDK may retry HTTP 503 internally before returning the error.
			failedRequests := remote.markerRequests()
			remote.mu.Lock()
			remote.markerStatus = 0
			remote.mu.Unlock()
			*now = now.Add(5 * time.Second)
			if _, err := runner.step(ctx, db); err != nil {
				t.Fatal(err)
			}
			if remote.markerRequests() != failedRequests+1 {
				t.Fatal("failed check was incorrectly deferred for five minutes")
			}
		})
	}
}
