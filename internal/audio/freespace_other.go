//go:build !linux

package audio

// Chronicle deploys on Linux. Elsewhere the volume figures are simply absent
// rather than guessed, and the report says so — an unqualified zero would read
// as a full disk.
func freeBytes(string) (free, total uint64, ok bool) { return 0, 0, false }
