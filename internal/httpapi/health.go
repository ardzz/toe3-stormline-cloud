package httpapi

import "net/http"

type healthJSON struct {
	Service          string `json:"service"`
	API              int    `json:"api"`
	Registration     string `json:"registration"`
	MaxSnapshotBytes int64  `json:"max_snapshot_bytes"`
}

// health describes the server, so a client can check a URL before logging in.
func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthJSON{Service: "toe3-cloud", API: 1, Registration: s.Config.Registration,
		MaxSnapshotBytes: s.Config.MaxSnapshotBytes})
}
