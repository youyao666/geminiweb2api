package main

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	mrand "math/rand"
	"sync/atomic"
	"time"
)

var requestID uint64

func init() {
	mrand.Seed(time.Now().UnixNano())
}

func generateUUIDv4() string {
	b := make([]byte, 16)
	cryptorand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func generateRandomIP() string {
	ips := []string{
		fmt.Sprintf("%d.%d.%d.%d", 1+mrand.Intn(126), mrand.Intn(256), mrand.Intn(256), 1+mrand.Intn(254)),
		fmt.Sprintf("%d.%d.%d.%d", 128+mrand.Intn(63), mrand.Intn(256), mrand.Intn(256), 1+mrand.Intn(254)),
		fmt.Sprintf("%d.%d.%d.%d", 192+mrand.Intn(31), mrand.Intn(256), mrand.Intn(256), 1+mrand.Intn(254)),
	}
	return ips[mrand.Intn(len(ips))]
}

func generateRandomHex(length int) string {
	b := make([]byte, length/2)
	cryptorand.Read(b)
	return hex.EncodeToString(b)
}

func getRequestID() uint64 {
	return atomic.AddUint64(&requestID, 1)
}
