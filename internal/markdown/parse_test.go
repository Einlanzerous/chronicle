package markdown

import "testing"

// ParseReference decides shape and never membership: a well-shaped KEY-N is a
// Switchyard reference whatever the key, the local namespaces are matched
// before anything else, and anything that is not exactly one reference is
// refused. CHRN-97 ruling 8.
func TestParseReferenceDecidesShapeAndNeverMembership(t *testing.T) {
	for _, tc := range []struct {
		token string
		want  Reference
		ok    bool
	}{
		{"SWY-389", Reference{System: SystemSwitchyard, Key: "SWY", Token: "SWY-389", Number: 389}, true},
		// No key set is consulted: SY is not a live project and this still
		// parses. The tracker answers membership with a 404.
		{"SY-412", Reference{System: SystemSwitchyard, Key: "SY", Token: "SY-412", Number: 412}, true},
		{"CHR-0311", Reference{System: SystemChronicle, Key: "CHR", Target: TargetNote, Token: "CHR-0311", Number: 311}, true},
		{"CHR-311", Reference{System: SystemChronicle, Key: "CHR", Target: TargetNote, Token: "CHR-311", Number: 311}, true},
		{"DSC-7", Reference{System: SystemChronicle, Key: "DSC", Target: TargetDiscussion, Token: "DSC-7", Number: 7}, true},
		{"amber1.9d654a7c.1ef5.0", Reference{System: SystemAmber, Token: "amber1.9d654a7c.1ef5.0"}, true},
		{"amber1.session.record", Reference{System: SystemAmber, Token: "amber1.session.record"}, true},

		{"", Reference{}, false},
		{"hello", Reference{}, false},
		{"chr-311", Reference{}, false},
		{"SWY-389 ", Reference{}, false},
		{" SWY-389", Reference{}, false},
		{"SWY-389x", Reference{}, false},
		{"xSWY-389", Reference{}, false},
		{"SWY-389/comments", Reference{}, false},
		{"see SWY-389 and CHR-0311", Reference{}, false},
		{"amber2.a.b", Reference{}, false},
		{"amber1.a", Reference{}, false},
	} {
		t.Run(tc.token, func(t *testing.T) {
			got, ok := ParseReference(tc.token)
			if ok != tc.ok {
				t.Fatalf("ParseReference(%q) ok = %v, want %v", tc.token, ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("ParseReference(%q) = %+v, want %+v", tc.token, got, tc.want)
			}
		})
	}
}

// And it agrees with Scan on every token Scan recognises: one grammar, not two.
func TestParseReferenceAgreesWithScan(t *testing.T) {
	src := []byte("See SWY-389, CHR-0311, DSC-7 and amber1.a.b.2 — but not UTF-8 in code: `SWY-1`.")
	r := NewRenderer(keySet{"SWY": true})
	for _, ref := range r.Scan(src).References {
		got, ok := ParseReference(ref.Token)
		if !ok || got != ref {
			t.Errorf("ParseReference(%q) = %+v, %v; Scan said %+v", ref.Token, got, ok, ref)
		}
	}
}
