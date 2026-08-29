package asr

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// The advisory lock, against a real Postgres. It is the case the in-process
// semaphore cannot see: TWO asrd PROCESSES, which is what a rolling redeploy
// produces for a few seconds every time.

func startLock(t *testing.T, deviceID string) (*DeviceLock, context.CancelFunc) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ASR_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("ASR_TEST_DATABASE_URL unset")
	}
	l := &DeviceLock{
		DSN:      dsn,
		DeviceID: deviceID,
		Logger:   discardLogger(),
		Poll:     50 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = l.Run(ctx)
	}()
	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the device lock did not stop")
		}
	}
	t.Cleanup(stop)
	return l, stop
}

// TWO PROCESSES, ONE DEVICE, ONE OWNER. The second is a standby: it claims
// nothing and loads no model, so it holds no VRAM either.
func TestOnlyOneProcessOwnsADevice(t *testing.T) {
	first, stopFirst := startLock(t, "test-device-one-owner")
	waitFor(t, "the first process to take the device", 10*time.Second, first.Held)

	second, _ := startLock(t, "test-device-one-owner")
	time.Sleep(500 * time.Millisecond)
	if second.Held() {
		t.Fatal("two processes hold the same device; a redeploy would run two inferences on one card")
	}

	// THE LOCK GOES WITH THE CONNECTION. Nothing runs in the departing
	// process to hand it over — which is the property that makes kill -9 and a
	// container stop identical here.
	stopFirst()
	waitFor(t, "the standby to be promoted", 10*time.Second, second.Held)
}

// A SECOND DEVICE IS A SECOND KEY, not a redesign. The first draft's single
// key per database meant one worker per Postgres, full stop: the desktop would
// have been a standby forever.
func TestADifferentDeviceIsADifferentLock(t *testing.T) {
	r9700, _ := startLock(t, "test-device-r9700")
	waitFor(t, "the first device", 10*time.Second, r9700.Held)

	desktop, _ := startLock(t, "test-device-desktop")
	waitFor(t, "the second device, which excludes nothing", 10*time.Second, desktop.Held)

	if !r9700.Held() {
		t.Fatal("taking a second device's lock released the first")
	}
}

// The key is stable across processes and versions. Go's maphash would not be —
// it is seeded per process, so two asrd processes would take different locks
// and both run, which is the exact failure this lock exists to prevent.
func TestTheDeviceKeyIsStable(t *testing.T) {
	if deviceLockKey("r9700") != deviceLockKey("r9700") {
		t.Fatal("the same device hashed to two keys")
	}
	if deviceLockKey("r9700") == deviceLockKey("desktop") {
		t.Fatal("two devices hashed to one key, so they would exclude each other")
	}
}
