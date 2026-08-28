-- 0001_jobs — CHRN-25. The transcription job, and the states it may pass through.
--
-- Decided before any code, in Mode B: docs/decisions/chrn-25-job-contract.md.
-- This file carries what the argument concluded, not the argument.
--
-- THIS IS A DIFFERENT DATABASE FROM CHRONICLE'S. Database `asr`, role `asr`,
-- provisioned by deploy/asr/provision-db.sh. It is not a schema inside
-- Chronicle and it is deliberately not reachable from Chronicle's role: the
-- whole reason E3 is an estate service rather than a Chronicle package is that
-- Catenary submits jobs too, and a job table in Chronicle's database would make
-- Catenary depend on Chronicle's schema.
--
-- IT IS ALSO NOT A TIER QUESTION. The tier split governs what lives in
-- CHRONICLE's two stores; job rows are a third thing — another service's own
-- state, in another service's database — and the tier rule does not reach
-- across that boundary in either direction. "The job table is tier 1" is the
-- plausible wrong summary, and acting on it puts these rows in the wrong
-- database.
--
-- Nothing here is irreplaceable. The submitted audio still exists on the client
-- side, so every row and every byte can be recomputed: drop this database and
-- the estate loses queue position and nothing else. The corollary a reviewer
-- should check on any change here is that NOTHING IN THIS SERVICE MAY BECOME
-- THE ONLY COPY OF ANYTHING. The moment it does, the store stops being
-- disposable and has quietly acquired the properties of tier 2 with none of the
-- protections.

CREATE TABLE IF NOT EXISTS jobs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Derived from the bearer token, never read from a body, header or query
    -- parameter. CHRN-26 queues per client for fairness, so a client-asserted
    -- identity would let either service submit as the other and jump its queue.
    client_id           TEXT NOT NULL CHECK (length(client_id) BETWEEN 1 AND 64),

    -- The client's handle for one transcription ATTEMPT, minted and persisted
    -- before the request was sent. Bounds match Chronicle's own ingest key
    -- (CHRN-18/CHRN-20) because it is the same header with the same meaning.
    idempotency_key     TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 16 AND 200),

    -- What the client says the audio hashes to, carried in the spec so the
    -- mismatch check does not depend on having buffered the body. DECLARED, not
    -- verified: a client that lies gets a job that transcribes bytes the
    -- contract believes are something else, which is a client bug this service
    -- cannot defend against and should not pretend to.
    audio_sha256        TEXT NOT NULL CHECK (audio_sha256 ~ '^[0-9a-f]{64}$'),
    audio_media_type    TEXT NOT NULL,

    -- The submitted bytes. Here rather than in a spool directory because a
    -- lease-expired job returns to `queued` needing them again, and a directory
    -- would reintroduce a filesystem dependency, a volume to mount, and a
    -- second cleanup path that can disagree with the first.
    --
    -- Deleted at terminal, which is what keeps "drop the database, lose nothing
    -- but queue position" literally true rather than approximately true.
    audio               BYTEA,
    audio_bytes         BIGINT NOT NULL CHECK (audio_bytes > 0),

    model               TEXT NOT NULL,
    language            TEXT,

    status              TEXT NOT NULL DEFAULT 'queued'
                          CHECK (status IN ('queued', 'leased', 'running',
                                            'succeeded', 'failed', 'cancelled')),

    -- Five stored states, not six: `cancelling` is DERIVED on the wire from
    -- `running` plus cancel_requested_at. See the column below.
    --
    -- `leased` is separate from `running` because they answer different
    -- questions — a worker has claimed the job, versus inference has actually
    -- started. Collapsing them makes a worker that died between claim and start
    -- indistinguishable from one that died mid-inference, and those want
    -- different attempts accounting.

    -- Incremented by the REAPER when a lease expires, not by the claim. How
    -- many retries and with what backoff is CHRN-28's policy; the counter lives
    -- here because the reaper is what moves it.
    attempts            INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),

    -- `kill -9` survival is a lease expiry, not a shutdown hook. A hook is not
    -- a mechanism: kill -9 does not run it, and that is the case the ticket
    -- names. The worker renews this while it works; the reaper returns any job
    -- whose lease has passed.
    lease_expires_at    TIMESTAMPTZ,
    leased_by           TEXT,

    -- Cancellation is a COLUMN, not a state. As a state it left `leased` with
    -- no cancel edge, and left one interaction undefined: a running job that
    -- was cancelled and whose worker then died would be reaped back to `queued`
    -- and RUN AGAIN — the one outcome cancel exists to prevent. See the reaper
    -- clause in the guard below.
    cancel_requested_at TIMESTAMPTZ,

    -- The result, as the wire shape the client will be handed. One column
    -- rather than a shredded set because nothing here queries inside it: the
    -- service writes it once and serves it back verbatim, and a second
    -- representation is a second thing to keep in step with the OpenAPI schema.
    result              JSONB,

    -- When the PAYLOAD is purged. Not the row: the row survives, which is what
    -- lets GET /v1/jobs/{id}/result answer 410 Gone rather than 404 — a client
    -- that comes back late learns its result expired, not that its job never
    -- existed. The idempotency uniqueness therefore still holds forever,
    -- because the row it is unique against is still here.
    result_purge_at     TIMESTAMPTZ,

    -- Denormalised out of `result` because it is the one field another service
    -- reasons about, and because stating it as a column is what lets the
    -- durable-transcript predicate be read at a glance:
    --
    --     a transcript is durable iff its job reached `succeeded`
    --     and `partial` is false.
    --
    -- It is a fact the SERVICE records about whether its own run completed. It
    -- is never derived from covered_ms < audio_duration_ms — whisper emits
    -- segments only where there is speech, so an ordinary memo with trailing
    -- silence has covered_ms short of its duration on a perfectly complete run,
    -- and a pruner gated on that would mark most of the corpus not-durable and
    -- never fire.
    partial             BOOLEAN,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,

    -- A terminal job holds no bytes; a live one must. Written as a constraint
    -- rather than left to the worker because "we always delete the audio at
    -- terminal" is a claim about every code path there will ever be, and this
    -- is the only place that can hold all of them.
    CONSTRAINT jobs_audio_present
        CHECK ((status IN ('succeeded', 'failed', 'cancelled')) = (audio IS NULL)),

    -- partial is meaningless before a run finishes, and required after a
    -- successful one — the predicate above has no answer for NULL.
    CONSTRAINT jobs_partial_when_succeeded
        CHECK (status <> 'succeeded' OR partial IS NOT NULL)
);

-- THE IDEMPOTENCY KEY IS AN INDEX, and the submit path inserts and handles the
-- conflict rather than checking first. CHRN-18's review found that a
-- check-then-insert "fails against a design that passes" its plain concurrency
-- test — and the race here is between two retries of ONE attempt, which is
-- precisely the situation this key exists for.
--
-- Client-scoped for the same reason Chronicle's arrival key is: a global unique
-- on a client-chosen string lets one client's key collide with another's and
-- deny it a job, through a namespace the two share for no reason.
CREATE UNIQUE INDEX IF NOT EXISTS jobs_client_key
    ON jobs (client_id, idempotency_key);

-- What the claim scans. Partial, because everything else is either in flight or
-- finished and the queue is the only thing polled in a loop.
CREATE INDEX IF NOT EXISTS jobs_queued
    ON jobs (created_at)
    WHERE status = 'queued';

-- What the reaper scans.
CREATE INDEX IF NOT EXISTS jobs_leases
    ON jobs (lease_expires_at)
    WHERE status IN ('leased', 'running');

-- What the result sweep scans.
CREATE INDEX IF NOT EXISTS jobs_result_purge
    ON jobs (result_purge_at)
    WHERE result IS NOT NULL;

-- The state machine lives here rather than in Go, for the reason Chronicle's
-- does: Go is not the only thing that will ever hold a connection to this
-- database. A psql session, a migration, a worker written in another language
-- when CHRN-29 splits this out — the edges hold for all of them, or they are
-- not edges.
CREATE OR REPLACE FUNCTION jobs_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
BEGIN
    -- One entry point. A job exists only once its audio has arrived whole, so
    -- there is no state meaning "maybe there are bytes".
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'queued' THEN
            RAISE EXCEPTION 'job must be created in status queued, got %', NEW.status
                USING ERRCODE = 'AS003';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.client_id       IS DISTINCT FROM OLD.client_id
    OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
    OR NEW.audio_sha256    IS DISTINCT FROM OLD.audio_sha256
    OR NEW.model           IS DISTINCT FROM OLD.model
    OR NEW.created_at      IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'job identity is immutable'
            USING ERRCODE = 'AS002';
    END IF;

    -- A JOB LEAVES THE QUEUE ONLY BY REACHING A TERMINAL STATE. The three
    -- terminal states appear only as targets and never as sources: that is how
    -- "terminal" is written down somewhere it can be read. Dropping a job is
    -- indistinguishable from a memo that was never captured, which is the
    -- failure the whole system exists to avoid.
    --
    -- 'leased>queued' and 'running>queued' are the reaper's edges, and they are
    -- the only way back.
    IF NEW.status IS DISTINCT FROM OLD.status
       AND (OLD.status || '>' || NEW.status) <> ALL (ARRAY[
             'queued>leased',      'queued>cancelled',
             'leased>running',     'leased>queued',      'leased>cancelled',
             'leased>failed',
             'running>succeeded',  'running>failed',     'running>cancelled',
             'running>queued'
       ]) THEN
        RAISE EXCEPTION 'illegal job status transition % -> %', OLD.status, NEW.status
            USING ERRCODE = 'AS001';
    END IF;

    -- The interaction the state-machine version of cancel got wrong. A job
    -- whose cancellation was requested must never be reaped back into the
    -- queue: it would be re-run, which is the one outcome cancel exists to
    -- prevent. The reaper sends these to `cancelled` instead, and this refuses
    -- the other answer rather than trusting every future caller to remember.
    IF NEW.status = 'queued' AND OLD.status IN ('leased', 'running')
       AND OLD.cancel_requested_at IS NOT NULL THEN
        RAISE EXCEPTION 'a cancelled job may not return to the queue'
            USING ERRCODE = 'AS004';
    END IF;

    -- Cancellation is a request, and a request is not withdrawn by this
    -- service. Clearing it is how the previous rule gets defeated in one line.
    IF OLD.cancel_requested_at IS NOT NULL
       AND NEW.cancel_requested_at IS DISTINCT FROM OLD.cancel_requested_at THEN
        RAISE EXCEPTION 'cancel_requested_at may not be changed once set'
            USING ERRCODE = 'AS002';
    END IF;

    NEW.updated_at := now();
    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER jobs_guard BEFORE INSERT OR UPDATE ON jobs
    FOR EACH ROW EXECUTE FUNCTION jobs_guard();

COMMENT ON TABLE jobs IS
  'CHRN-25. One row per transcription attempt, kept. Unbounded by design; at '
  'estate scale that is nothing. Holds nothing irreplaceable — the client still '
  'has the audio — so this whole database is safe to drop.';
