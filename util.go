package sentry

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"io/ioutil"
	"log"
)

var debugger = log.New(ioutil.Discard, "[Sentry]", log.LstdFlags)

func uuid() string {
	id := make([]byte, 16)
	_, _ = io.ReadFull(rand.Reader, id)
	id[6] &= 0x0F // clear version
	id[6] |= 0x40 // set version to 4 (random uuid)
	id[8] &= 0x3F // clear variant
	id[8] |= 0x80 // set to IETF variant
	return hex.EncodeToString(id)
}
