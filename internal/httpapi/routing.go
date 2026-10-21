package httpapi

import (
	"maps"
	"net/http"
	"slices"
	"strings"
)

// methods routes a path's requests by HTTP method and answers any other method with 405 and an Allow header.
type methods map[string]http.HandlerFunc

func (m methods) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h, ok := m[r.Method]; ok {
		h(w, r)
		return
	}
	w.Header().Set("Allow", strings.Join(slices.Sorted(maps.Keys(m)), ", "))
	writeError(w, apiError{Status: http.StatusMethodNotAllowed, Code: "method_not_allowed",
		Message: "method not allowed on this resource"})
}

// notFound answers requests for paths the API does not serve.
func notFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, apiError{Status: http.StatusNotFound, Code: "not_found", Message: "no such resource"})
}
