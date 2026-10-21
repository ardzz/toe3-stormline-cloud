package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ardzz/toe3-stormline-cloud/internal/save"
)

// expectedHead reads the upload precondition: If-None-Match: * (no saves yet, head 0) or If-Match: "<rev>" with the
// revision in canonical decimal form, as ^"[1-9][0-9]*"$. Exactly one of them, with one value, is accepted.
func expectedHead(h http.Header) (int64, error) {
	match, none := h.Values("If-Match"), h.Values("If-None-Match")
	switch {
	case len(match) == 0 && len(none) == 0:
		return 0, errPreconditionRequired
	case len(match)+len(none) > 1:
		return 0, errBadRequest
	case len(none) == 1:
		if strings.TrimSpace(none[0]) != "*" {
			return 0, errBadRequest
		}
		return 0, nil
	}
	tag := strings.TrimSpace(match[0])
	if len(tag) < 3 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		return 0, errBadRequest
	}
	digits := tag[1 : len(tag)-1]
	if digits[0] < '1' || digits[0] > '9' || strings.TrimLeft(digits, "0123456789") != "" {
		return 0, errBadRequest
	}
	rev, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, errBadRequest
	}
	return rev, nil
}

// uploadMeta reads the X-Toe3-* description of an upload; the save service validates the values. A missing game
// build is 0.
func uploadMeta(h http.Header) (save.Meta, error) {
	m := save.Meta{SHA256: h.Get("X-Toe3-Sha256"), Device: h.Get("X-Toe3-Device"), Reason: h.Get("X-Toe3-Reason")}
	if v := h.Get("X-Toe3-Game-Build"); v != "" {
		build, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return save.Meta{}, save.ErrInvalidMeta
		}
		m.GameBuild = build
	}
	return m, nil
}
