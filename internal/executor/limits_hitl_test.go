package executor

import (
	"testing"
	"time"

	"github.com/phrony-platform/runtime/internal/manifest"
)

func TestLimitTracker_hitlWaitExcludedFromWallClock(t *testing.T) {
	maxWall := 1
	limits := &manifest.Limits{MaxWallClockSeconds: &maxWall}
	tracker := newLimitTracker(limits)
	tracker.beginHITLWait()
	time.Sleep(1100 * time.Millisecond)
	if err := tracker.endHITLWait(); err != nil {
		t.Fatalf("endHITLWait() = %v", err)
	}
	if err := tracker.checkWallClock(); err != nil {
		t.Fatalf("checkWallClock() = %v, want nil while HITL time excluded", err)
	}
}

func TestLimitTracker_maxHITLWaitMinutes(t *testing.T) {
	max := 1
	limits := &manifest.Limits{MaxHITLWaitMinutes: &max}
	tracker := newLimitTracker(limits)
	tracker.hitlWaitAccum = 2 * time.Minute
	if err := tracker.endHITLWait(); err == nil {
		t.Fatal("endHITLWait() = nil, want hitl limit error")
	}
}
