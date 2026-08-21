package restore

import "syscall"

// makeFifo is the only way to get a non-regular, non-directory file into a test
// tree without privileges.
func makeFifo(path string) error { return syscall.Mkfifo(path, 0o600) }
