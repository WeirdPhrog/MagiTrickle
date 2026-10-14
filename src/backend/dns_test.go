package magitrickle

import (
	"testing"
	"time"
)

func TestRemainingTTLNeverTurnsLiveRecordsPermanent(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		remaining time.Duration
		want      uint32
	}{
		{-time.Second, 0}, {0, 0}, {time.Nanosecond, 1}, {time.Second, 1}, {time.Second + time.Nanosecond, 2},
	} {
		if got := remainingTTL(now.Add(test.remaining), now); got != test.want {
			t.Errorf("TTL for %v = %d, want %d", test.remaining, got, test.want)
		}
	}
}
