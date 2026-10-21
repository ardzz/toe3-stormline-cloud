package httpapi

import (
	"encoding/json"
	"net/http"
)

// maxJSONBody bounds JSON request bodies; credentials are far smaller.
const maxJSONBody = 8 << 10

// writeJSON sends v as the JSON body with status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the status is already sent; a failed write means the client has gone away
}

// readJSON decodes a small JSON request body into dst; any problem is errBadRequest.
func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(dst); err != nil {
		return errBadRequest
	}
	return nil
}
