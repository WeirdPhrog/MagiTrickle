package recordsCache

import (
	"net"
	"slices"
	"sync"
	"testing"
)

func TestAddressSnapshotsRemainStable(t *testing.T) {
	r := New()
	ip := net.IP{192, 0, 2, 1}
	r.AddAddress("EXAMPLE.com", ip, 120)
	before := r.GetAddresses("example.COM")
	if len(before) != 1 {
		t.Fatal("DNS names must be case insensitive")
	}
	deadline := before[0].Deadline
	ip[0] = 10
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			r.AddAddress("example.com", net.IP{192, 0, 2, 1}, 300)
		}
	}()
	for i := 0; i < 1000; i++ {
		if before[0].Deadline != deadline || before[0].Address[0] != 192 {
			t.Error("snapshot mutated after returning from cache")
			break
		}
	}
	wg.Wait()
}

func TestExpiredAliasesAreNotUsedBeforeCleanup(t *testing.T) {
	r := New()
	r.AddAlias("EXPIRED.com", "TARGET.com", 0)
	r.AddAlias("VALID.com", "target.COM", 60)
	aliases := r.GetAliases("TARGET.com")
	if slices.Contains(aliases, "expired.com") || !slices.Contains(aliases, "valid.com") {
		t.Fatalf("unexpected aliases: %v", aliases)
	}
}
