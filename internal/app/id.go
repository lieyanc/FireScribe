package app

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

func newID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "_" + hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
