package randutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// RandomSuffix returns a short random hex string suitable for unique naming.
func RandomSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fall back to timestamp-based suffix if crypto/rand fails
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	return hex.EncodeToString(b)
}
