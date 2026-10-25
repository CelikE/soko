//go:build !windows

package journal

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockJournalFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX)
}
