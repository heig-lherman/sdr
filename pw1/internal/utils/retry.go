package utils

import (
	"chatsapp/internal/logging"
	"context"
	"math"
	"time"
)

const maxAttempts = 10
const maxDelay = 60

type Retryable func(try int) error

func RetryWithCutoff(
	log *logging.Logger,
	ctx context.Context,
	fn Retryable,
) (err error) {
	for i := 0; i < maxAttempts; i++ {
		err = fn(i)
		if err == nil {
			return
		}

		delay := time.Duration(math.Min(math.Pow(2, float64(i)), maxDelay)) * time.Second
		log.Errorf("Error: %v [Retrying in %v...]", err, delay)

		t := time.NewTimer(delay)
		select {
		case <-t.C:
		case <-ctx.Done():
			if !t.Stop() {
				<-t.C
			}

			err = ctx.Err()
			return
		}
	}
	return
}
