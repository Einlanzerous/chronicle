package mcp

import "time"

// SetNow replaces the hosted transport's clock, so a test can make a session
// idle or its token nearly expired without waiting an hour.
func (h *Hosted) SetNow(now func() time.Time) { h.now = now }
