//go:build windows

package store

// diskFree returns zero when the platform-specific free-space probe is not
// available. Callers treat zero as unknown and still enforce max spool bytes.
func diskFree(path string) (int64, error) { return 0, nil }
