//go:build !unix

package store

import "os"

func acquireLock(path string) (*os.File, error) {
	return os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
}
