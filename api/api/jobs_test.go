package api

import (
	"DataArk/jobqueue"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestModuleRecoveryFailureDoesNotStopSharedWorkers(t *testing.T) {
	for _, failing := range []string{"discovery", "dedupe", "assessment", "recommendation", "batch"} {
		t.Run(failing, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			var workers sync.WaitGroup
			defer func() { cancel(); workers.Wait() }()
			executed := make(chan struct{}, 1)
			handlers := jobqueue.Handlers{GenerateDaily: func(context.Context, uint, string) error { executed <- struct{}{}; return nil }}
			stop, err := jobqueue.Start(ctx, nil, handlers, func(ctx context.Context, q jobqueue.JobEnqueuer) error {
				startModuleRecoveries(ctx, []moduleRecovery{
					{name: failing, run: func(context.Context) error { return errors.New("injected module recovery failure") }},
					{name: "independent", run: func(ctx context.Context) error { return q.EnqueueGenerateDaily(ctx, 1, "2026-11-05") }},
				}, &workers)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			if _, ok := jobqueue.Default(); !ok {
				t.Fatal("default queue removed by module failure")
			}
			select {
			case <-executed:
			case <-time.After(2 * time.Second):
				t.Fatal("unrelated worker did not execute")
			}
		})
	}
}
