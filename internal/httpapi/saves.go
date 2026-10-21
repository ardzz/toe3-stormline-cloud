package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/save"
)

type revisionJSON struct {
	Rev       int64     `json:"rev"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	Device    string    `json:"device"`
	Reason    string    `json:"reason"`
	GameBuild int64     `json:"game_build"`
}

func toRevisionJSON(r save.Revision) revisionJSON {
	return revisionJSON{Rev: r.Rev, CreatedAt: r.CreatedAt.UTC(), Size: r.Size, SHA256: r.SHA256, Device: r.Device,
		Reason: r.Reason, GameBuild: r.GameBuild}
}

// list answers with the account's committed revisions, newest first.
func (s *server) list(w http.ResponseWriter, r *http.Request, userID int64) {
	revs, err := s.Saves.List(r.Context(), userID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]revisionJSON, 0, len(revs))
	for _, rev := range revs {
		out = append(out, toRevisionJSON(rev))
	}
	writeJSON(w, http.StatusOK, struct {
		Revisions []revisionJSON `json:"revisions"`
	}{out})
}

// latest answers with the head revision's snapshot.
func (s *server) latest(w http.ResponseWriter, r *http.Request, userID int64) {
	rev, data, err := s.Saves.Latest(r.Context(), userID)
	s.sendSnapshot(w, r, rev, data, err)
}

// get answers with one revision's snapshot; a path that is not a positive revision number is not found.
func (s *server) get(w http.ResponseWriter, r *http.Request, userID int64) {
	rev, err := strconv.ParseInt(r.PathValue("rev"), 10, 64)
	if err != nil || rev < 1 {
		s.fail(w, r, save.ErrNotFound)
		return
	}
	got, data, err := s.Saves.Get(r.Context(), userID, rev)
	s.sendSnapshot(w, r, got, data, err)
}

// sendSnapshot answers with a snapshot exactly as uploaded. It is a gzip file, not a gzip-encoded response, so no
// Content-Encoding is set and HTTP clients do not decompress it.
func (s *server) sendSnapshot(w http.ResponseWriter, r *http.Request, rev save.Revision, data []byte, err error) {
	if err != nil {
		s.fail(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/gzip")
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("ETag", etag(rev.Rev))
	h.Set("X-Toe3-Sha256", rev.SHA256)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data) // a failed write means the client has gone away
}

// etag is a revision number as a strong entity tag.
func etag(rev int64) string { return `"` + strconv.FormatInt(rev, 10) + `"` }
