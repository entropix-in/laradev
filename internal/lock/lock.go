package lock

import (
	"context"
	"time"

	"github.com/gofrs/flock"
)

type Lock struct{ f *flock.Flock }

func Acquire(ctx context.Context, path string, exclusive bool, timeout time.Duration) (*Lock, error) {
	f := flock.New(path)
	deadline := time.Now().Add(timeout)
	for {
		var ok bool
		var err error
		if exclusive {
			ok, err = f.TryLock()
		} else {
			ok, err = f.TryRLock()
		}
		if err != nil {
			return nil, err
		}
		if ok {
			return &Lock{f: f}, nil
		}
		if time.Now().After(deadline) {
			return nil, context.DeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func (l *Lock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	return l.f.Unlock()
}
