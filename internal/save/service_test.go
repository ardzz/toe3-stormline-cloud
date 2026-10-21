package save

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.FixedZone("WIB", 7*3600))

func newTestService(store *scriptedStore, blobs *memBlobs) *Service {
	return NewService(store, blobs, Options{MaxBytes: 1 << 20, Retention: 20, Now: func() time.Time { return testNow }})
}

func validUpload(t *testing.T) ([]byte, Meta) {
	data := gzipped(t, "snapshot")
	return data, Meta{SHA256: shaHex(data), Device: "Pixel 7", Reason: "manual", GameBuild: 193}
}

func TestUploadStoresBlobAndCommits(t *testing.T) {
	store, blobs := &scriptedStore{}, newMemBlobs()
	data, meta := validUpload(t)
	r, err := newTestService(store, blobs).Upload(context.Background(), 7, 2, data, meta)
	if err != nil {
		t.Fatal(err)
	}
	want := Revision{Rev: 3, CreatedAt: testNow.UTC(), Size: int64(len(data)), SHA256: meta.SHA256, Device: "Pixel 7",
		Reason: "manual", GameBuild: 193, BlobKey: "u/7/3.json.gz"}
	if r != want {
		t.Fatalf("Upload = %+v, want %+v", r, want)
	}
	if !bytes.Equal(blobs.data["u/7/3.json.gz"], data) {
		t.Fatal("snapshot not stored under the reserved key")
	}
	if !slices.Equal(store.commits, [][4]int64{{7, 2, 3, 20}}) || len(store.aborts) != 0 {
		t.Fatalf("commits %v, aborts %v", store.commits, store.aborts)
	}
}

func TestUploadValidatesBeforeStorage(t *testing.T) {
	store, blobs := &scriptedStore{}, newMemBlobs()
	svc := newTestService(store, blobs)
	data, meta := validUpload(t)
	if _, err := svc.Upload(context.Background(), 7, -1, data, meta); !errors.Is(err, ErrInvalidMeta) {
		t.Fatalf("negative head: %v", err)
	}
	meta.SHA256 = strings.Repeat("0", 64)
	if _, err := svc.Upload(context.Background(), 7, 0, data, meta); !errors.Is(err, ErrSHAMismatch) {
		t.Fatalf("bad digest: %v", err)
	}
	if len(store.reserved) != 0 || len(blobs.data) != 0 {
		t.Fatal("invalid upload reached storage")
	}
}

func TestUploadPassesStaleReservation(t *testing.T) {
	store, blobs := &scriptedStore{reserveErr: &StaleError{Latest: 4}}, newMemBlobs()
	data, meta := validUpload(t)
	_, err := newTestService(store, blobs).Upload(context.Background(), 7, 2, data, meta)
	var se *StaleError
	if !errors.As(err, &se) || se.Latest != 4 || len(blobs.data) != 0 {
		t.Fatalf("err = %v, blobs %d; want StaleError{4} and nothing stored", err, len(blobs.data))
	}
}

func TestUploadAbortsWhenBlobWriteFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store, blobs := &scriptedStore{}, newMemBlobs()
	blobs.putErr = context.Canceled
	data, meta := validUpload(t)
	_, err := newTestService(store, blobs).Upload(ctx, 7, 0, data, meta)
	if !errors.Is(err, context.Canceled) || !slices.Equal(store.aborts, [][2]int64{{7, 1}}) || len(store.commits) != 0 {
		t.Fatalf("err %v, aborts %v, commits %v", err, store.aborts, store.commits)
	}
	if !slices.Equal(store.abortCtxErrs, []error{nil}) {
		t.Fatalf("abort context errors = %v; want [nil]", store.abortCtxErrs)
	}
}

func TestUploadLeavesCleanupToTheStoreOnCommitFailures(t *testing.T) {
	for _, commitErr := range []error{ErrReservationLost, &StaleError{Latest: 9}, errors.New("connection reset")} {
		store, blobs := &scriptedStore{commitErr: commitErr}, newMemBlobs()
		data, meta := validUpload(t)
		_, err := newTestService(store, blobs).Upload(context.Background(), 7, 0, data, meta)
		if !errors.Is(err, commitErr) || len(blobs.deleted) != 0 || len(blobs.data) != 1 {
			t.Fatalf("%v: err %v, deleted %v", commitErr, err, blobs.deleted)
		}
	}
}

func TestLatestAndGet(t *testing.T) {
	ctx := context.Background()
	data := gzipped(t, "snapshot")
	rev := Revision{Rev: 2, BlobKey: "u/7/2.json.gz", SHA256: shaHex(data)}
	store, blobs := &scriptedStore{head: rev, byRev: rev}, newMemBlobs()
	blobs.data[rev.BlobKey] = data
	svc := newTestService(store, blobs)
	if r, got, err := svc.Latest(ctx, 7); err != nil || r != rev || !bytes.Equal(got, data) {
		t.Fatalf("Latest = %+v, %d bytes, %v", r, len(got), err)
	}
	if r, got, err := svc.Get(ctx, 7, 2); err != nil || r != rev || !bytes.Equal(got, data) {
		t.Fatalf("Get = %+v, %d bytes, %v", r, len(got), err)
	}
	delete(blobs.data, rev.BlobKey)
	if _, _, err := svc.Latest(ctx, 7); !errors.Is(err, ErrBlobMissing) {
		t.Fatalf("Latest with missing blob = %v", err)
	}
	blobs.getErr = errors.New("read failed")
	if _, _, err := svc.Latest(ctx, 7); !errors.Is(err, blobs.getErr) || errors.Is(err, ErrBlobMissing) {
		t.Fatalf("Latest with read error = %v; want read failed, not ErrBlobMissing", err)
	}
	store.headErr, store.byRevErr = ErrNoSaves, ErrNotFound
	if _, _, err := svc.Latest(ctx, 7); !errors.Is(err, ErrNoSaves) {
		t.Fatalf("Latest without saves = %v", err)
	}
	if _, _, err := svc.Get(ctx, 7, 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get unknown = %v", err)
	}
}

func TestUploadReportsAbortFailure(t *testing.T) {
	boom, abortBoom := errors.New("disk full"), errors.New("abort failed")
	store, blobs := &scriptedStore{abortErr: abortBoom}, newMemBlobs()
	blobs.putErr = boom
	data, meta := validUpload(t)
	_, err := newTestService(store, blobs).Upload(context.Background(), 7, 0, data, meta)
	if !errors.Is(err, boom) || !errors.Is(err, abortBoom) {
		t.Fatalf("Upload = %v; want both write and abort errors", err)
	}
}

func TestUploadBoundsBlobWriteByPendingTTL(t *testing.T) {
	store, blobs := &scriptedStore{}, newMemBlobs()
	data, meta := validUpload(t)
	if _, err := newTestService(store, blobs).Upload(context.Background(), 7, 0, data, meta); err != nil {
		t.Fatal(err)
	}
	want := []time.Time{testNow.UTC().Add(time.Hour)}
	if !slices.Equal(blobs.putDeadlines, want) {
		t.Fatalf("Put deadlines = %v; want %v", blobs.putDeadlines, want)
	}
}
