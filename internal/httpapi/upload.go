package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/ardzz/toe3-stormline-cloud/internal/save"
)

// upload stores the request body as the account's new head revision.
func (s *server) upload(w http.ResponseWriter, r *http.Request, userID int64) {
	head, err := expectedHead(r.Header)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if r.ContentLength > s.Config.MaxSnapshotBytes {
		s.fail(w, r, save.ErrTooLarge)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.Config.MaxSnapshotBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if !errors.As(err, &tooBig) {
			err = errBadRequest
		}
		s.fail(w, r, err)
		return
	}
	meta, err := uploadMeta(r.Header)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rev, err := s.Saves.Upload(r.Context(), userID, head, data, meta)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", etag(rev.Rev))
	writeJSON(w, http.StatusCreated, toRevisionJSON(rev))
}
