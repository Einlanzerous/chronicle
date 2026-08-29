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

// THE KEY IS PINNED, and the constant below is the point of the test.
//
// It has to be stable across processes AND ACROSS VERSIONS: a rolling redeploy
// runs two asrd builds against one card for a few seconds, and if the newer one
// hashes `r9700` differently the two do not exclude each other at all — they
// both take a lock, both claim, and both run an inference on the device. That
// is the exact failure this lock exists to prevent, arriving through a change
// that looks like an implementation detail.
//
// So changing the hash is a BREAKING CHANGE that needs a drained deploy, and
// this test is what says so. (Go's maphash would have failed it on the first
// run: it is seeded per process.)
func TestTheDeviceKeyIsPinned(t *testing.T) {
	const r9700 = 6147738836272930545
	if got := deviceLockKey("r9700"); got != r9700 {
		t.Fatalf("deviceLockKey(\"r9700\") is %d, was %d. Two asrd versions overlapping "+
			"during a redeploy would now take DIFFERENT locks and both run on the card. "+
			"If this is deliberate, it needs a drained deploy rather than a rolling one.", got, r9700)
	}
	if deviceLockKey("r9700") == deviceLockKey("desktop") {
		t.Fatal("two devices hashed to one key, so they would exclude each other")
	}
}
