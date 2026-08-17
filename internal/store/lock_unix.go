//go:build unix

package store

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another eggbot process already holds %s", path)
	}
	return f, nil
}
