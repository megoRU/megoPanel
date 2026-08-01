package platform

import (
	"crypto/rand"
	"fmt"
	"time"
)

// NewUUIDv7 generates a lightweight RFC 9562-compliant UUID version 7.
// A UUIDv7 is composed of:
// - 48-bit timestamp (milliseconds since epoch)
// - 4-bit version (0111 = 7)
// - 12-bit sequence/random space
// - 2-bit variant (10xx = RFC 4122/9562 variant)
// - 62-bit random space
func NewUUIDv7() (string, error) {
	var value [16]byte
	timestamp := uint64(time.Now().UnixMilli())

	// 48-bit timestamp (bytes 0 to 5)
	value[0] = byte(timestamp >> 40)
	value[1] = byte(timestamp >> 32)
	value[2] = byte(timestamp >> 24)
	value[3] = byte(timestamp >> 16)
	value[4] = byte(timestamp >> 8)
	value[5] = byte(timestamp)

	// Random bytes for the remaining 10 bytes
	_, err := rand.Read(value[6:])
	if err != nil {
		return "", err
	}

	// version 7: set bits 4-7 of byte 6 to 0111
	value[6] = (value[6] & 0x0f) | 0x70
	// variant 1: set bits 6-7 of byte 8 to 10
	value[8] = (value[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:],
	), nil
}
