package main

import (
	"context"
	"errors"
)

const maxOutcomeDrainPasses = 32

type outcomeOnceRunner interface {
	RunOnce(context.Context) (int, error)
}

func drainOutcomeWorker(ctx context.Context, runner outcomeOnceRunner, batchSize int) (int, error) {
	if runner == nil {
		return 0, errors.New("delivery outcome reconciliation worker is required")
	}
	if batchSize <= 0 {
		return 0, errors.New("delivery outcome reconciliation batch size must be positive")
	}
	total := 0
	for pass := 0; pass < maxOutcomeDrainPasses; pass++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		processed, err := runner.RunOnce(ctx)
		total += processed
		if err != nil {
			return total, err
		}
		if processed < batchSize {
			return total, nil
		}
	}
	return total, nil
}
