package dispatch

import (
	"context"
	"errors"
	"time"

	"campaign-platform/internal/storage"
)

type SignedObjectResolver struct{ Signer storage.URLSigner }

func (r SignedObjectResolver) Resolve(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, err := r.Signer.Sign(key, ttl)
	if err != nil {
		return "", errors.New("sign media object URL")
	}
	return value, nil
}
