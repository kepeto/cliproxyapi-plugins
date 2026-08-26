package shared

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartRetriesEmptyCatalog(t *testing.T) {
	var calls atomic.Int32
	refreshed := make(chan struct{})
	refresher := NewModelRefresher(time.Hour, func() ([]string, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("temporary outage")
		}
		close(refreshed)
		return []string{"model-a"}, nil
	}, nil)
	refresher.retryInterval = time.Millisecond
	defer refresher.Stop()

	refresher.Start()
	select {
	case <-refreshed:
	case <-time.After(time.Second):
		t.Fatal("refresher did not retry an empty catalog")
	}
	if err := refresher.RefreshIfEmpty(); err != nil {
		t.Fatal(err)
	}
	if models := refresher.Models(); len(models) != 1 || models[0] != "model-a" {
		t.Fatalf("unexpected models after retry: %#v", models)
	}
}

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

func TestModelRefresherStopIsIdempotentAndWaits(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	refresher := NewModelRefresher(time.Hour, func() ([]string, error) {
		calls.Add(1)
		close(started)
		<-release
		return []string{"model-a"}, nil
	}, nil)

	refresher.Start()
	refresher.Start()
	<-started
	stopped := make(chan struct{})
	go func() {
		refresher.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Stop returned before the in-flight fetch completed")
	default:
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not wait for the refresher goroutine")
	}
	refresher.Stop()
	refresher.Start()
	if calls.Load() != 1 {
		t.Fatalf("fetch calls = %d, want one lifecycle start", calls.Load())
	}

	beforeStart := NewModelRefresher(time.Hour, func() ([]string, error) {
		t.Fatal("stopped-before-start refresher fetched")
		return nil, nil
	}, nil)
	beforeStart.Stop()
	beforeStart.Start()
	beforeStart.Stop()
}
