package save

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func gzipped(t testing.TB, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func shaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestDescribeAcceptsValidUpload(t *testing.T) {
	data := gzipped(t, `{"format":"toe3-prefs-snapshot"}`)
	m := Meta{SHA256: strings.ToUpper(shaHex(data)), Device: " Pixel\x007 ", Reason: "manual", GameBuild: 193}
	r, err := describe(data, m, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	want := Revision{Size: int64(len(data)), SHA256: shaHex(data), Device: "Pixel7", Reason: "manual", GameBuild: 193}
	if r != want {
		t.Fatalf("describe = %+v, want %+v", r, want)
	}
}

func TestDescribeRejects(t *testing.T) {
	data := gzipped(t, "snapshot")
	ok := Meta{SHA256: shaHex(data), Reason: "auto"}
	with := func(f func(*Meta)) Meta {
		m := ok
		f(&m)
		return m
	}
	plain := []byte("plain text")
	cases := []struct {
		name string
		data []byte
		meta Meta
		max  int64
		want error
	}{
		{"empty", nil, ok, 1 << 20, ErrEmpty},
		{"too large", data, ok, int64(len(data)) - 1, ErrTooLarge},
		{"not gzip", plain, Meta{SHA256: shaHex(plain), Reason: "auto"}, 1 << 20, ErrNotGzip},
		{"short sha", data, with(func(m *Meta) { m.SHA256 = "abc" }), 1 << 20, ErrInvalidMeta},
		{"empty reason", data, with(func(m *Meta) { m.Reason = "" }), 1 << 20, ErrInvalidMeta},
		{"upper reason", data, with(func(m *Meta) { m.Reason = "Manual" }), 1 << 20, ErrInvalidMeta},
		{"long reason", data, with(func(m *Meta) { m.Reason = "a-reason-longer-than-16" }), 1 << 20, ErrInvalidMeta},
		{"negative build", data, with(func(m *Meta) { m.GameBuild = -1 }), 1 << 20, ErrInvalidMeta},
		{"huge build", data, with(func(m *Meta) { m.GameBuild = 1 << 31 }), 1 << 20, ErrInvalidMeta},
		{"sha mismatch", data, with(func(m *Meta) { m.SHA256 = strings.Repeat("0", 64) }), 1 << 20, ErrSHAMismatch},
	}
	for _, c := range cases {
		if _, err := describe(c.data, c.meta, c.max); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}
