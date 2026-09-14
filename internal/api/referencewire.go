package api

import (
	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/resolve"
)

// The reference payloads, mapped between CHRN-48's Reference, CHRN-51's
// Resolution and the generated types.
//
// Transcribed rather than aliased, for the reason triagewire.go gives at
// length: a document that describes whatever the Go struct happens to say is a
// guard that cannot fail. Neither domain type ever carried json tags -- neither
// was ever on the wire before this ticket -- so the projection is stated as a
// table in TestEveryResolutionFieldReachesTheDocument and checked both ways
// there, which is how a field added to Resolution and not to openapi.yaml is
// caught before three generated clients never see it.

// toDescriptor renders a scanned reference as the note payload carries it.
// CHRN-98 emits these; ResolveReferences takes them back.
func toDescriptor(ref markdown.Reference) wire.ReferenceDescriptor {
	d := wire.ReferenceDescriptor{
		System: wire.ReferenceDescriptorSystem(ref.System),
		Token:  ref.Token,
		Key:    optional(ref.Key),
	}
	if ref.Target != "" {
		t := wire.ReferenceDescriptorTarget(ref.Target)
		d.Target = &t
	}
	// Absent for Amber, whose citations carry no number -- and present for
	// every KEY-N form, including a written `SWY-0`, so the test is the
	// system and not the value.
	if ref.System != markdown.SystemAmber {
		n := ref.Number
		d.Number = &n
	}
	return d
}

// descriptorAgrees reports whether what a client said about a token matches
// what the grammar derives from it (CHRN-97 ruling 8).
//
// `system` is required on the wire and always compared, because it is the
// field that would pick the transport. The other three are compared when
// present: a client may honestly omit what the note payload omitted, and
// omitting is not disagreeing.
func descriptorAgrees(d wire.ReferenceDescriptor, ref markdown.Reference) bool {
	if string(d.System) != ref.System {
		return false
	}
	if d.Key != nil && *d.Key != ref.Key {
		return false
	}
	if d.Target != nil && string(*d.Target) != ref.Target {
		return false
	}
	if d.Number != nil && *d.Number != ref.Number {
		return false
	}
	return true
}

// toResolution serialises CHRN-51's card. Not a field added, not a state
// collapsed; the one projection is Ref onto its token, which holds because a
// token is unique within one scan's output.
func toResolution(res resolve.Resolution) wire.Resolution {
	out := wire.Resolution{
		Token:          res.Ref.Token,
		State:          wire.ResolutionState(res.State),
		Explain:        optional(res.Explain),
		LastResolvedAt: res.LastResolvedAt,
	}
	// ABSENT ON THE TWO STATES WHERE NOTHING WAS ATTEMPTED (ruling 7). The Go
	// type is zero there by convention and says so in capitals; a convention
	// does not cross a language boundary, so the wire omits it by shape and a
	// generated client cannot format a zero instant as an age.
	if !res.FetchedAt.IsZero() {
		t := res.FetchedAt
		out.FetchedAt = &t
	}
	if u := res.Upstream; u != nil {
		out.Upstream = &wire.ResolutionUpstream{
			Key:         optional(u.Key),
			Outcome:     optional(u.Outcome),
			DisplayName: optional(u.DisplayName),
			Title:       optional(u.Title),
			Url:         optional(u.URL),
		}
		// Amber's, and only Amber's. On every other system it is a zero
		// nobody set, and a client reading it would learn nothing true.
		if res.Ref.System == markdown.SystemAmber {
			r := u.Recoverable
			out.Upstream.Recoverable = &r
		}
	}
	return out
}
