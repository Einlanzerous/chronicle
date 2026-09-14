package api

import (
	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// E6's types, mapped onto the generated ones; checked both ways by the
// projection tables in contract_test.go.

// toDiscussion renders a thread beside the path it is filed at, when it is,
// and the ref of the note it resolved into, when it did. The row carries the
// note's id; a client addresses a note by its ref, so the caller resolves it
// (resolvedNoteRef) and hands it in.
func toDiscussion(d store.Discussion, path, noteRef *string) wire.Discussion {
	out := wire.Discussion{Ref: d.Ref(), Title: d.Title, CreatedAt: d.CreatedAt, Page: path}
	if d.ResolvedAt != nil && d.ResolvedBy != nil {
		out.Resolved = &wire.ResolvedState{At: *d.ResolvedAt, By: *d.ResolvedBy, Note: noteRef}
	}
	return out
}

func toParticipant(p store.DiscussionParticipant) wire.Participant {
	return wire.Participant{
		UserId:      p.UserID,
		Kind:        wire.ParticipantKind(p.Kind),
		DisplayName: p.DisplayName,
		AddedAt:     p.AddedAt,
		AddedBy:     p.AddedBy,
		RemovedAt:   p.RemovedAt,
		RemovedBy:   p.RemovedBy,
		LastReadSeq: p.LastReadSeq,
		LastReadAt:  p.LastReadAt,
	}
}
