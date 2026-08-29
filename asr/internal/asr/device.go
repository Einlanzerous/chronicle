package asr

import (
	"context"
	"hash/fnv"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// DeviceLock is the answer to "which asrd owns the GPU": a Postgres advisory
// lock, keyed by hash(ASR_DEVICE_ID), held for the PROCESS'S LIFETIME.
//
// It is not CHRN-25's job lease and the difference is the whole design. The job
// lease is a TIMESTAMP, chosen so it depends on no connection. This is
// CONNECTION-SCOPED, chosen so that a process which dies cannot keep the
// device: Postgres drops the lock with the session, so container stop, kill -9
// and a panic all release it without anything running in the dying process.
//
// What it protects against is the case the in-process semaphore cannot see: TWO
// asrd PROCESSES. A rolling redeploy overlaps old and new, and both would
// otherwise start an inference on the same card.
//
// Everything below follows from "connection-scoped":
//
//   - It is held on a DEDICATED pgx.Conn, outside the pool. A pooled connection
//     is the pool's to close, and the lock would go with it.
//   - Loss of that connection is loss of ownership. A Postgres restart drops
//     every session and every lock; this notices on the next check, drops to
//     standby, and re-acquires before claiming again.
//   - The check is pg_try_advisory_lock, NEVER the blocking form. A query that
//     blocks forever on one connection is invisible to everything, /readyz
//     included.
//   - A host crash sends no FIN, so the session survives until Postgres's TCP
//     keepalive notices — and the OS default for that is 7200 s. The keepalive
//     settings below bound it to about a minute, session-locally, with no
//     change to the shared Postgres.
type DeviceLock struct {
	// DSN is the ASR database. The lock lives on a connection of its own, so
	// this is dialled separately from the pool the rest of the service uses.
	DSN string

	// DeviceID names the GPU, not the deployment. Advisory locks are scoped to
	// the DATABASE, so two deployments on one Postgres have two `asr`
	// databases and cannot collide however they are keyed; what a key can
	// usefully distinguish is one card from another, which is what makes a
	// second worker a value rather than a redesign (CHRN-80).
	DeviceID string

	Logger *slog.Logger

	// Poll is how often ownership is re-checked when held, and re-attempted
	// when not. It is deliberately the same interval for both: a standby is
	// polling for a promotion and an owner is polling for a dropped session,
	// and neither wants to be slower than the other.
	Poll time.Duration

	mu        sync.Mutex
	conn      *pgx.Conn
	held      bool
	announced bool
}

// deviceLockKey hashes a device id into the bigint pg_try_advisory_lock takes.
//
// FNV-1a rather than anything cryptographic: this is a namespace, not a secret,
// and what it must be is STABLE ACROSS PROCESSES AND VERSIONS. Go's maphash
// would not be — it is seeded per process, so two asrd processes would take
// different locks and both run.
func deviceLockKey(deviceID string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(deviceID))
	return int64(h.Sum64())
}

// Held reports whether this process currently owns the device.
//
// Read by the worker before every claim and by /readyz. A process that does not
// own it is a STANDBY: it claims nothing and loads no model, so it holds no
// VRAM either.
func (l *DeviceLock) Held() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.held
}

// Run acquires the lock and keeps checking that it is still ours until ctx is
// cancelled.
//
// It returns nil rather than an error when it cannot get the lock: a standby is
// a correct state, not a failure, and a process that exited because another one
// held the device would flap through a redeploy.
func (l *DeviceLock) Run(ctx context.Context) error {
	poll := l.Poll
	if poll <= 0 {
		poll = 5 * time.Second
	}
	key := deviceLockKey(l.DeviceID)
	l.Logger.Info("device lock starting", "device", l.DeviceID, "key", key, "poll", poll.String())

	defer l.release()

	for {
		l.check(ctx, key)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(poll):
		}
	}
}

// check is one pass: connect if needed, then either verify the lock we hold or
// try once for the one we do not.
func (l *DeviceLock) check(ctx context.Context, key int64) {
	l.mu.Lock()
	conn, held := l.conn, l.held
	l.mu.Unlock()

	if conn == nil {
		var err error
		conn, err = l.dial(ctx)
		if err != nil {
			if ctx.Err() == nil {
				l.Logger.Warn("could not open the device lock connection; not claiming",
					"device", l.DeviceID, "error", err)
			}
			l.set(nil, false)
			return
		}
		l.set(conn, false)
		held = false
	}

	if held {
		// The cheapest possible "is this session still alive". The lock is
		// released by the server when the session goes, so a live session with
		// the lock is the whole of the ownership claim.
		if _, err := conn.Exec(ctx, `SELECT 1`); err != nil {
			if ctx.Err() != nil {
				return
			}
			l.Logger.Warn("LOST THE DEVICE LOCK: its connection went away. Not claiming until it is back",
				"device", l.DeviceID, "error", err)
			_ = conn.Close(context.WithoutCancel(ctx))
			l.set(nil, false)
		}
		return
	}

	var ok bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&ok); err != nil {
		if ctx.Err() != nil {
			return
		}
		l.Logger.Warn("could not take the device lock", "device", l.DeviceID, "error", err)
		_ = conn.Close(context.WithoutCancel(ctx))
		l.set(nil, false)
		return
	}
	if !ok {
		l.announceStandby()
		return
	}
	l.set(conn, true)
	l.Logger.Info("device lock acquired; this process owns the device",
		"device", l.DeviceID, "key", key)
}

// dial opens the dedicated connection and bounds how long a host crash can hold
// the lock after the machine is gone.
func (l *DeviceLock) dial(ctx context.Context) (*pgx.Conn, error) {
	conn, err := pgx.Connect(ctx, l.DSN)
	if err != nil {
		return nil, err
	}
	// About a minute rather than the OS default of 7200 s. Three statements
	// rather than one: pgx sends these over the extended protocol, which takes
	// a single command at a time.
	for _, stmt := range []string{
		`SET tcp_keepalives_idle = 30`,
		`SET tcp_keepalives_interval = 10`,
		`SET tcp_keepalives_count = 3`,
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			_ = conn.Close(context.WithoutCancel(ctx))
			return nil, err
		}
	}
	return conn, nil
}

func (l *DeviceLock) set(conn *pgx.Conn, held bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.conn, l.held = conn, held
	if held {
		l.announced = false
	}
}

// announceStandby says it once. A standby polls forever, and one line per poll
// would bury the log of whatever else the process is doing.
func (l *DeviceLock) announceStandby() {
	l.mu.Lock()
	first := !l.announced
	l.announced = true
	l.mu.Unlock()
	if first {
		l.Logger.Info("another process holds this device; running as STANDBY: "+
			"serving the API, claiming no jobs, loading no model",
			"device", l.DeviceID)
	}
}

// release closes the lock connection, which is what actually releases the lock.
// Explicit on a clean shutdown so a redeploy's new process can take the device
// without waiting for a TCP timeout.
func (l *DeviceLock) release() {
	l.mu.Lock()
	conn, held := l.conn, l.held
	l.conn, l.held = nil, false
	l.mu.Unlock()
	if conn == nil {
		return
	}
	if held {
		l.Logger.Info("releasing the device lock", "device", l.DeviceID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = conn.Close(ctx)
}
