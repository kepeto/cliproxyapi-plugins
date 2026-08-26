package shared

import (
	"sync"
	"testing"
	"time"
)

func TestModelProbeSchedulerHidesAndRestores(t *testing.T) {
	health := NewModelHealth(3, time.Hour)
	target := ModelProbeTarget{Scope: "provider", Model: "model"}
	failed := make(chan struct{})
	signalFailed := sync.OnceFunc(func() { close(failed) })
	scheduler := NewModelProbeScheduler(time.Millisecond, health, func() []ModelProbeTarget {
		return []ModelProbeTarget{target}
	}, func(ModelProbeTarget) ModelProbeOutcome {
		signalFailed()
		return ProbeFailed
	})
	scheduler.Start()
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("probe did not run on schedule")
	}
	if !health.Hidden(target.Scope, target.Model) {
		t.Fatal("failed probe did not hide model")
	}
	scheduler.Stop()
	scheduler.Stop()

	recovered := make(chan struct{})
	signalRecovered := sync.OnceFunc(func() { close(recovered) })
	recovery := NewModelProbeScheduler(time.Millisecond, health, func() []ModelProbeTarget {
		return []ModelProbeTarget{target}
	}, func(ModelProbeTarget) ModelProbeOutcome {
		signalRecovered()
		return ProbeSucceeded
	})
	recovery.Start()
	select {
	case <-recovered:
	case <-time.After(time.Second):
		t.Fatal("recovery probe did not run on schedule")
	}
	recovery.Stop()
	if health.Hidden(target.Scope, target.Model) {
		t.Fatal("successful probe did not restore model")
	}
}
