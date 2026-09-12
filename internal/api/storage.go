package api

import (
	"context"
	"github.com/google/uuid"
	"net/http"
	"time"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// Corpus is the slice of the store the storage report needs.
type Corpus interface {
	AudioInventory(ctx context.Context) ([]store.AudioRef, error)
	CorpusStats(ctx context.Context, window time.Duration) (store.CorpusStats, error)

	// RetentionStatus is CHRN-22 §3: what will happen to one memo's audio, and
	// when. The same clause the pruner sweeps with, so the date a client
	// renders is the date the job will use.
	RetentionStatus(ctx context.Context, memoID uuid.UUID, window time.Duration) (string, *time.Time, error)
}

// listSample bounds how many individual refs the report names. The counts are
// always exact; the lists are a sample so a corpus-wide problem produces a
// readable answer rather than a megabyte of JSON.
const listSample = 20

// GetStorageReport answers "what does the corpus cost, and does the disk
// agree with the database" — CHRN-23's "a number the service reports rather
// than one someone runs du for".
//
// Owner-only, and reachable only on the Access-gated host: deploy/traefik
// keeps /admin off the WAN entrypoint entirely. It is a read: it deletes
// nothing, and it never hands the pruner a list. Orphans are reported so a
// human can decide, which is a different thing from a job acting on them.
//
// The payload types are GENERATED from openapi.yaml into internal/api/wire.
// They used to live here, as storageReport and its four children; a
// hand-written response struct beside a document describing the same response
// is exactly the drift CHRN-97's guard exists to make impossible.
func (a *api) GetStorageReport(w http.ResponseWriter, r *http.Request) {
	if a.audio == nil || a.corpus == nil {
		writeError(w, http.StatusServiceUnavailable, codeAudioUnconfigured,
			"storage accounting is not configured: set CHRONICLE_AUDIO_DIR")
		return
	}

	stats, err := a.corpus.CorpusStats(r.Context(), audio.ProjectionWindow)
	if err != nil {
		a.serverError(w, r, "corpus stats", err)
		return
	}
	inv, err := a.corpus.AudioInventory(r.Context())
	if err != nil {
		a.serverError(w, r, "audio inventory", err)
		return
	}

	// The walk is bounded by the corpus, which the sizing puts in the hundreds
	// of files. If that stops being true this is the first place it will hurt,
	// and the fix is a cached scan rather than a coarser report.
	onDisk, err := a.audio.Scan()
	if err != nil {
		a.serverError(w, r, "scan audio store", err)
		return
	}

	want := make(audio.Expected, len(inv))
	for _, ref := range inv {
		want[audio.Ref{AuthorID: ref.AuthorID, ContentHash: ref.ContentHash}] = ref.ByteSize
	}
	rec := audio.Reconcile(onDisk, want)
	vol := a.audio.Volume()

	rep := wire.StorageReport{
		Root: a.audio.Root(),
		Disk: wire.DiskReport{
			Files:            len(onDisk.Files),
			Bytes:            onDisk.Bytes,
			Strays:           len(onDisk.Strays),
			StrayBytes:       onDisk.StrayBytes,
			StraySample:      firstN(onDisk.Strays, listSample),
			Staging:          onDisk.Staging,
			StagingBytes:     onDisk.StagingBytes,
			VolumeKnown:      vol.Known,
			VolumeTotalBytes: vol.TotalBytes,
			VolumeFreeBytes:  vol.FreeBytes,
		},
		Corpus: wire.CorpusReport{
			Memos:         stats.Memos,
			AudioPresent:  stats.AudioPresent,
			AudioPruned:   stats.AudioPruned,
			RecordedBytes: stats.RecordedBytes,
			EverBytes:     stats.EverBytes,
			OldestCapture: stats.OldestCapture,
			NewestCapture: stats.NewestCapture,
		},
		Window: wire.WindowReport{
			Days:           int(audio.ProjectionWindow / (24 * time.Hour)),
			Memos:          stats.WindowMemos,
			Bytes:          stats.WindowBytes,
			ProjectedBytes: audio.ProjectedWindowBytes,
			PctOfProjected: pctOf(stats.WindowBytes, audio.ProjectedWindowBytes),
		},
		Reconciliation: wire.ReconciliationReport{
			Orphans:          len(rec.Orphans),
			OrphanBytes:      rec.OrphanBytes,
			OrphanSample:     firstN(refStrings(rec.Orphans), listSample),
			Missing:          len(rec.Missing),
			MissingBytes:     rec.MissingBytes,
			MissingSample:    firstN(refStrings(rec.Missing), listSample),
			Mismatched:       len(rec.Mismatched),
			MismatchedSample: firstNMismatch(mismatches(rec.Mismatched), listSample),
		},
	}

	// Missing audio is not a report detail. A memo that expects its recording
	// and cannot find it is the failure CLAUDE.md calls unrecoverable, so it
	// leaves a log line whether or not anyone reads the response.
	if len(rec.Missing) > 0 {
		a.logger.ErrorContext(r.Context(), "audio missing from disk for memos that expect it",
			"count", len(rec.Missing), "bytes", rec.MissingBytes, "root", a.audio.Root())
	}

	writeJSON(w, http.StatusOK, rep)
}

func pctOf(got, of int64) float64 {
	if of == 0 {
		return 0
	}
	return float64(got) / float64(of) * 100
}

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func refStrings(refs []audio.Ref) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.AuthorID.String()+"/"+r.ContentHash)
	}
	return out
}

// mismatches renders a mismatch with the numbers that make it one.
func mismatches(ms []audio.Mismatch) []wire.Mismatch {
	out := make([]wire.Mismatch, 0, len(ms))
	for _, m := range ms {
		out = append(out, wire.Mismatch{
			Ref:           m.Ref.AuthorID.String() + "/" + m.Ref.ContentHash,
			OnDiskBytes:   m.OnDisk,
			RecordedBytes: m.Recorded,
		})
	}
	return out
}

func firstNMismatch(s []wire.Mismatch, n int) []wire.Mismatch {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
