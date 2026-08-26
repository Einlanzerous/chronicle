package api

import (
	"context"
	"net/http"
	"time"

	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// Corpus is the slice of the store the storage report needs.
type Corpus interface {
	AudioInventory(ctx context.Context) ([]store.AudioRef, error)
	CorpusStats(ctx context.Context, window time.Duration) (store.CorpusStats, error)
}

// listSample bounds how many individual refs the report names. The counts are
// always exact; the lists are a sample so a corpus-wide problem produces a
// readable answer rather than a megabyte of JSON.
const listSample = 20

type storageReport struct {
	Root           string       `json:"root"`
	Disk           diskReport   `json:"disk"`
	Corpus         corpusReport `json:"corpus"`
	Window         windowReport `json:"window"`
	Reconciliation reconReport  `json:"reconciliation"`
}

type diskReport struct {
	Files      int   `json:"files"`
	Bytes      int64 `json:"bytes"`
	Strays     int   `json:"strays"`
	StrayBytes int64 `json:"stray_bytes"`
	// StraySample names files under the root that this layout did not write.
	// They are never counted as corpus and never offered to the pruner.
	StraySample []string `json:"stray_sample,omitempty"`

	// No omitempty on the two figures: a full volume has FreeBytes == 0, and
	// omitempty would delete the field at exactly the moment it matters most,
	// leaving "volume_known": true beside no measurement. VolumeKnown is what
	// separates "not measured" from "measured as zero" — let it do that job.
	VolumeKnown bool  `json:"volume_known"`
	VolumeTotal int64 `json:"volume_total_bytes"`
	VolumeFree  int64 `json:"volume_free_bytes"`
}

type corpusReport struct {
	Memos         int64      `json:"memos"`
	AudioPresent  int64      `json:"audio_present"`
	AudioPruned   int64      `json:"audio_pruned"`
	RecordedBytes int64      `json:"recorded_bytes"`
	EverBytes     int64      `json:"ever_bytes"`
	OldestCapture *time.Time `json:"oldest_capture"`
	NewestCapture *time.Time `json:"newest_capture"`
}

type windowReport struct {
	Days           int     `json:"days"`
	Memos          int64   `json:"memos"`
	Bytes          int64   `json:"bytes"`
	ProjectedBytes int64   `json:"projected_bytes"`
	PctOfProjected float64 `json:"pct_of_projected"`
}

type reconReport struct {
	Orphans      int      `json:"orphans"`
	OrphanBytes  int64    `json:"orphan_bytes"`
	OrphanSample []string `json:"orphan_sample,omitempty"`

	// Missing is the direction that matters: a memo says its audio is present
	// and the file is not there.
	Missing       int      `json:"missing"`
	MissingBytes  int64    `json:"missing_bytes"`
	MissingSample []string `json:"missing_sample,omitempty"`

	Mismatched int `json:"mismatched"`
	// The sample carries both sizes. Without them a 5-against-4096 truncation
	// and a 4097-against-4096 rounding read identically, and those are not the
	// same finding.
	MismatchedSample []mismatchJSON `json:"mismatched_sample,omitempty"`
}

// handleAdminStorage answers "what does the corpus cost, and does the disk
// agree with the database" — CHRN-23's "a number the service reports rather
// than one someone runs du for".
//
// Owner-only, and reachable only on the Access-gated host: deploy/traefik
// keeps /admin off the WAN entrypoint entirely. It is a read: it deletes
// nothing, and it never hands the pruner a list. Orphans are reported so a
// human can decide, which is a different thing from a job acting on them.
func (a *api) handleAdminStorage(w http.ResponseWriter, r *http.Request) {
	if a.audio == nil || a.corpus == nil {
		http.Error(w, "storage accounting is not configured: set CHRONICLE_AUDIO_DIR", http.StatusServiceUnavailable)
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

	rep := storageReport{
		Root: a.audio.Root(),
		Disk: diskReport{
			Files:       len(onDisk.Files),
			Bytes:       onDisk.Bytes,
			Strays:      len(onDisk.Strays),
			StrayBytes:  onDisk.StrayBytes,
			StraySample: firstN(onDisk.Strays, listSample),
			VolumeKnown: vol.Known,
			VolumeTotal: vol.TotalBytes,
			VolumeFree:  vol.FreeBytes,
		},
		Corpus: corpusReport{
			Memos:         stats.Memos,
			AudioPresent:  stats.AudioPresent,
			AudioPruned:   stats.AudioPruned,
			RecordedBytes: stats.RecordedBytes,
			EverBytes:     stats.EverBytes,
			OldestCapture: stats.OldestCapture,
			NewestCapture: stats.NewestCapture,
		},
		Window: windowReport{
			Days:           int(audio.ProjectionWindow / (24 * time.Hour)),
			Memos:          stats.WindowMemos,
			Bytes:          stats.WindowBytes,
			ProjectedBytes: audio.ProjectedWindowBytes,
			PctOfProjected: pctOf(stats.WindowBytes, audio.ProjectedWindowBytes),
		},
		Reconciliation: reconReport{
			Orphans:          len(rec.Orphans),
			OrphanBytes:      rec.OrphanBytes,
			OrphanSample:     firstN(refStrings(rec.Orphans), listSample),
			Missing:          len(rec.Missing),
			MissingBytes:     rec.MissingBytes,
			MissingSample:    firstN(refStrings(rec.Missing), listSample),
			Mismatched:       len(rec.Mismatched),
			MismatchedSample: firstNMismatch(mismatchJSONs(rec.Mismatched), listSample),
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

// mismatchJSON is a mismatch with the numbers that make it one.
type mismatchJSON struct {
	Ref      string `json:"ref"`
	OnDisk   int64  `json:"on_disk_bytes"`
	Recorded int64  `json:"recorded_bytes"`
}

func mismatchJSONs(ms []audio.Mismatch) []mismatchJSON {
	out := make([]mismatchJSON, 0, len(ms))
	for _, m := range ms {
		out = append(out, mismatchJSON{
			Ref:      m.Ref.AuthorID.String() + "/" + m.Ref.ContentHash,
			OnDisk:   m.OnDisk,
			Recorded: m.Recorded,
		})
	}
	return out
}

func firstNMismatch(s []mismatchJSON, n int) []mismatchJSON {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
