//go:build !darwin && !dragonfly && !freebsd && !illumos && !linux && !netbsd && !openbsd && !solaris

package configfile

// syncDirectory is unavailable or not meaningful on platforms without a
// supported directory descriptor sync operation. The temporary file itself
// is still synced before replacement.
func syncDirectory(string) error {
	return nil
}
