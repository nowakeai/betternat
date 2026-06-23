package awscloud

import (
	"errors"
	"testing"
)

func TestRetryerUsesBoundedControlPlanePolicy(t *testing.T) {
	retryer := newRetryer()
	if retryer.MaxAttempts() != defaultMaxAttempts {
		t.Fatalf("MaxAttempts = %d, want %d", retryer.MaxAttempts(), defaultMaxAttempts)
	}
	for attempt := 1; attempt <= 12; attempt++ {
		delay, err := retryer.RetryDelay(attempt, errors.New("throttled"))
		if err != nil {
			t.Fatalf("retry delay attempt %d: %v", attempt, err)
		}
		if delay > defaultMaxBackoff {
			t.Fatalf("retry delay attempt %d = %s, want <= %s", attempt, delay, defaultMaxBackoff)
		}
	}
}
