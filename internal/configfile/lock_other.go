//go:build !darwin && !dragonfly && !freebsd && !illumos && !linux && !netbsd && !openbsd && !solaris && !windows

package configfile

// acquireConfigLock is a best-effort fallback for platforms without a
// supported kernel locking API in x/sys. The package still serializes updates
// within one process; callers on these platforms should provide process-level
// coordination if they share one config across processes.
func acquireConfigLock(string) (func(), error) {
	return func() {}, nil
}
