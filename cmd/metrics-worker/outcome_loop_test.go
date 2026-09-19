package main

import (
	"context"
	"errors"
	"testing"
)

type outcomeRunResult struct {
	count int
	err   error
}

type outcomeOnceStub struct {
	results []outcomeRunResult
	calls   int
}

func (s *outcomeOnceStub) RunOnce(context.Context) (int, error) {
	s.calls++
	if len(s.results) == 0 {
		return 0, nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result.count, result.err
}

func TestDrainOutcomeWorkerRunsUntilPartialBatch(t *testing.T) {
	stub := &outcomeOnceStub{results: []outcomeRunResult{{count: 500}, {count: 500}, {count: 12}}}
	total, err := drainOutcomeWorker(context.Background(), stub, 500)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1012 || stub.calls != 3 {
		t.Fatalf("drain total=%d calls=%d want total=1012 calls=3", total, stub.calls)
	}
}

func TestDrainOutcomeWorkerFailsClosed(t *testing.T) {
	want := errors.New("postgres unavailable")
	stub := &outcomeOnceStub{results: []outcomeRunResult{{count: 500}, {err: want}}}
	total, err := drainOutcomeWorker(context.Background(), stub, 500)
	if !errors.Is(err, want) {
		t.Fatalf("drain err=%v want %v", err, want)
	}
	if total != 500 || stub.calls != 2 {
		t.Fatalf("drain total=%d calls=%d want total=500 calls=2", total, stub.calls)
	}
}
