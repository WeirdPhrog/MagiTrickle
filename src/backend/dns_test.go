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
		{time.Duration(maxRoutingTTL+1) * time.Second, maxRoutingTTL},
	} {
		if got := remainingTTL(now.Add(test.remaining), now); got != test.want {
			t.Errorf("TTL for %v = %d, want %d", test.remaining, got, test.want)
		}
	}
}

func TestRoutingTTLStaysFiniteWithoutOverflow(t *testing.T) {
	for _, test := range []struct {
		ttl        uint32
		additional time.Duration
		want       uint32
	}{
		{0, 0, 1},
		{0, time.Hour, 3600},
		{120, time.Hour, 3720},
		{120, 500 * time.Millisecond, 121},
		{120, -time.Second, 120},
		{maxRoutingTTL, time.Second, maxRoutingTTL},
		{^uint32(0), time.Hour, maxRoutingTTL},
		{1, time.Duration(1<<63 - 1), maxRoutingTTL},
	} {
		if got := routingTTL(test.ttl, test.additional); got != test.want {
			t.Errorf("TTL %d + %v = %d, want %d", test.ttl, test.additional, got, test.want)
		}
	}
}
