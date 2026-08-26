//go:build linux

package audio

import "syscall"

// freeBytes reports the volume holding path.
//
// stdlib syscall rather than golang.org/x/sys: this is one call with a stable
// shape on the one platform Chronicle deploys to, and CLAUDE.md's dependency
// list is short on purpose.
func freeBytes(path string) (free, total uint64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	// Bavail, not Bfree: the reserved blocks only root can use are not space
	// this service will ever get, and a budget that counts them reports
	// headroom that is not there.
	return st.Bavail * uint64(st.Bsize), st.Blocks * uint64(st.Bsize), true
}
