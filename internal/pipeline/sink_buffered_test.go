package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"go-xwatch/internal/journal"
)

type recordingSink struct {
	batches  [][]journal.Entry
	failOnce bool
}

func (r *recordingSink) Handle(_ context.Context, entries []journal.Entry) error {
	if r.failOnce {
		r.failOnce = false
		return errors.New("boom")
	}
	copyBatch := append([]journal.Entry(nil), entries...)
	r.batches = append(r.batches, copyBatch)
	return nil
}

func (r *recordingSink) Close(context.Context) error { return nil }

func TestBufferedSinkFlushByBatch(t *testing.T) {
	rec := &recordingSink{}
	sink := NewBufferedSink(rec, time.Second, 2)
	entries := []journal.Entry{{Op: "a"}, {Op: "b"}}
	if err := sink.Handle(context.Background(), entries); err != nil {
		t.Fatalf("handle err: %v", err)
	}
	if len(rec.batches) != 1 || len(rec.batches[0]) != 2 {
		t.Fatalf("expected 1 batch of 2, got %+v", rec.batches)
	}
}

func TestBufferedSinkFlushByWindow(t *testing.T) {
	rec := &recordingSink{}
	sink := NewBufferedSink(rec, 20*time.Millisecond, 10)
	if err := sink.Handle(context.Background(), []journal.Entry{{Op: "a"}}); err != nil {
		t.Fatalf("handle err: %v", err)
	}
	time.Sleep(25 * time.Millisecond)
	if err := sink.Handle(context.Background(), nil); err != nil {
		t.Fatalf("second handle err: %v", err)
	}
	if len(rec.batches) != 1 || len(rec.batches[0]) != 1 {
		t.Fatalf("expected 1 batch flushed by window, got %+v", rec.batches)
	}
}

func TestBufferedSinkRetriesOnError(t *testing.T) {
	rec := &recordingSink{failOnce: true}
	sink := NewBufferedSink(rec, time.Second, 2)
	if err := sink.Handle(context.Background(), []journal.Entry{{Op: "a"}, {Op: "b"}}); err == nil {
		t.Fatalf("expected error on first flush")
	}
	if len(rec.batches) != 0 {
		t.Fatalf("unexpected batches after failure: %+v", rec.batches)
	}
	if err := sink.Handle(context.Background(), nil); err != nil {
		t.Fatalf("second handle err: %v", err)
	}
	if len(rec.batches) != 1 || len(rec.batches[0]) != 2 {
		t.Fatalf("expected buffered entries to flush after retry, got %+v", rec.batches)
	}
}

func TestBufferedSinkCloseFlushes(t *testing.T) {
	rec := &recordingSink{}
	sink := NewBufferedSink(rec, time.Second, 10)
	if err := sink.Handle(context.Background(), []journal.Entry{{Op: "a"}, {Op: "b"}}); err != nil {
		t.Fatalf("handle err: %v", err)
	}
	if len(rec.batches) != 0 {
		t.Fatalf("expected no flush before close")
	}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatalf("close err: %v", err)
	}
	if len(rec.batches) != 1 || len(rec.batches[0]) != 2 {
		t.Fatalf("expected buffered flush on close, got %+v", rec.batches)
	}
}
