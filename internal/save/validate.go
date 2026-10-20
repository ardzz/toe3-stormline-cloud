package save

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"regexp"
	"strings"

	"github.com/ardzz/toe3-stormline-cloud/internal/label"
)

const maxDeviceRunes = 64

var (
	sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
	reasonRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,15}$`)
)

// describe validates an upload and returns its Revision without Rev, BlobKey and CreatedAt. The snapshot is opaque:
// only its size, gzip magic and digest are checked, it is never decompressed.
func describe(data []byte, m Meta, maxBytes int64) (Revision, error) {
	switch {
	case len(data) == 0:
		return Revision{}, ErrEmpty
	case int64(len(data)) > maxBytes:
		return Revision{}, ErrTooLarge
	case len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b:
		return Revision{}, ErrNotGzip
	}
	want := strings.ToLower(m.SHA256)
	if !sha256RE.MatchString(want) || !reasonRE.MatchString(m.Reason) || m.GameBuild < 0 || m.GameBuild > math.MaxInt32 {
		return Revision{}, ErrInvalidMeta
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return Revision{}, ErrSHAMismatch
	}
	return Revision{Size: int64(len(data)), SHA256: want, Device: label.Clean(m.Device, maxDeviceRunes),
		Reason: m.Reason, GameBuild: m.GameBuild}, nil
}
