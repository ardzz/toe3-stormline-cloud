package save

import (
	"errors"
	"fmt"
)

var (
	ErrNoSaves         = errors.New("save: no saves yet")
	ErrNotFound        = errors.New("save: revision not found")
	ErrEmpty           = errors.New("save: snapshot is empty")
	ErrTooLarge        = errors.New("save: snapshot is too large")
	ErrNotGzip         = errors.New("save: snapshot is not gzip data")
	ErrSHAMismatch     = errors.New("save: sha256 does not match the snapshot")
	ErrInvalidMeta     = errors.New("save: invalid upload metadata")
	ErrBlobMissing     = errors.New("save: stored snapshot is missing")
	ErrReservationLost = errors.New("save: pending upload no longer exists")
)

// StaleError refuses an upload whose expected head is not the account's current head.
type StaleError struct{ Latest int64 }

func (e *StaleError) Error() string { return fmt.Sprintf("save: head revision is %d", e.Latest) }
