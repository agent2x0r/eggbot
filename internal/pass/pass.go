// Package pass hashes userfile passwords with argon2id.
package pass

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

const (
	timeCost    = 1
	memoryKiB   = 64 * 1024
	parallelism = 4
	keyLen      = 32
	saltLen     = 16
)

var (
	hashSem   = make(chan struct{}, 2)
	dummyOnce sync.Once
	dummyHash string
)

func Hash(password string) (string, error) {
	hashSem <- struct{}{}
	defer func() { <-hashSem }()
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, timeCost, memoryKiB, parallelism, keyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		memoryKiB, timeCost, parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func Verify(password, encoded string) bool {
	hashSem <- struct{}{}
	defer func() { <-hashSem }()
	salt, want, t, m, p, err := parse(encoded)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, uint8(p), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func DummyVerify(password string) {
	dummyOnce.Do(func() {
		dummyHash, _ = Hash("eggbot-dummy-verify")
	})
	if dummyHash == "" {
		return
	}
	_ = Verify(password, dummyHash)
}

func parse(enc string) (salt, key []byte, time, memory uint32, parallel int, err error) {
	// $argon2id$v=19$m=65536,t=1,p=4$SALT$KEY
	parts := strings.Split(enc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil, nil, 0, 0, 0, fmt.Errorf("invalid hash")
	}
	var t, m, p int
	if _, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return nil, nil, 0, 0, 0, err
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, 0, 0, 0, err
	}
	key, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, 0, 0, 0, err
	}
	return salt, key, uint32(t), uint32(m), p, nil
}
