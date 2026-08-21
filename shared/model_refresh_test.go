package shared

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshIfEmptyWaitsForInflightRefresh(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	refresher := NewModelRefresher(time.Hour, func() ([]string, error) {
		calls.Add(1)
		close(started)
		<-release
		return []string{"model-a"}, nil
	}, nil)

	refreshDone := make(chan error, 1)
	go func() { refreshDone <- refresher.Refresh() }()
	<-started

	ensureDone := make(chan error, 1)
	go func() { ensureDone <- refresher.RefreshIfEmpty() }()
	close(release)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	if err := <-ensureDone; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("fetch called %d times, want 1", calls.Load())
	}
	if models := refresher.Models(); len(models) != 1 || models[0] != "model-a" {
		t.Fatalf("unexpected models: %#v", models)
	}
}
