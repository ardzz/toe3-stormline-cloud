// Package blobtest is the contract suite every blob.Store implementation must pass.
package blobtest

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/ardzz/toe3-stormline-cloud/internal/blob"
)

// Run checks store against the blob.Store contract. The store must start empty.
func Run(t *testing.T, store blob.Store) {
	ctx := context.Background()
	t.Run("PutGetRoundTrip", func(t *testing.T) {
		data := make([]byte, 1<<20)
		if _, err := rand.Read(data); err != nil {
			t.Fatal(err)
		}
		if err := store.Put(ctx, "u/1/1.json.gz", data); err != nil {
			t.Fatal(err)
		}
		if got, err := store.Get(ctx, "u/1/1.json.gz"); err != nil || !bytes.Equal(got, data) {
			t.Fatalf("Get = %d bytes, %v; want the %d bytes written", len(got), err, len(data))
		}
	})
	t.Run("PutReplaces", func(t *testing.T) {
		for _, v := range []string{"first", "second"} {
			if err := store.Put(ctx, "u/1/2.json.gz", []byte(v)); err != nil {
				t.Fatal(err)
			}
		}
		if got, err := store.Get(ctx, "u/1/2.json.gz"); err != nil || string(got) != "second" {
			t.Fatalf("Get after replace = %q, %v", got, err)
		}
	})
	t.Run("MissingKey", func(t *testing.T) {
		if _, err := store.Get(ctx, "u/9/9.json.gz"); !errors.Is(err, blob.ErrNotFound) {
			t.Fatalf("Get(missing) = %v, want ErrNotFound", err)
		}
		if err := store.Delete(ctx, "u/9/9.json.gz"); err != nil {
			t.Fatalf("Delete(missing) = %v, want nil", err)
		}
	})
	t.Run("Delete", func(t *testing.T) {
		if err := store.Put(ctx, "u/2/1.json.gz", []byte("x")); err != nil {
			t.Fatal(err)
		}
		if err := store.Delete(ctx, "u/2/1.json.gz"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Get(ctx, "u/2/1.json.gz"); !errors.Is(err, blob.ErrNotFound) {
			t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
		}
	})
	t.Run("RejectsInvalidKeys", func(t *testing.T) {
		for _, key := range []string{"", "../escape", "u/../../escape", "/abs", "U/1"} {
			if err := store.Put(ctx, key, []byte("x")); !errors.Is(err, blob.ErrInvalidKey) {
				t.Errorf("Put(%q) = %v, want ErrInvalidKey", key, err)
			}
			if _, err := store.Get(ctx, key); !errors.Is(err, blob.ErrInvalidKey) {
				t.Errorf("Get(%q) = %v, want ErrInvalidKey", key, err)
			}
			if err := store.Delete(ctx, key); !errors.Is(err, blob.ErrInvalidKey) {
				t.Errorf("Delete(%q) = %v, want ErrInvalidKey", key, err)
			}
		}
	})
}
