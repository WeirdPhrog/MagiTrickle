//go:build integration && linux

package netfilterTools

import "testing"

func TestRealRoutingLargeIPSetReplacement(t *testing.T) {
	set := (&Helper{IpsetPrefix: "large_"}).IPSet("sub")
	if err := set.Enable(); err != nil {
		t.Fatal(err)
	}
	defer set.Disable()
	addresses := make(map[IPv4Subnet]IPSetTimeout, 65537)
	for i := 0; i < 65537; i++ {
		addresses[IPv4Subnet{Address: [4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}, CIDR: 32}] = nil
	}
	if err := set.Replace(addresses, nil); err != nil {
		t.Fatal(err)
	}
	stored, err := set.ListIPv4Subnets()
	if err != nil || len(stored) != len(addresses) {
		t.Fatalf("large subscription truncated: %d/%d: %v", len(stored), len(addresses), err)
	}
	if err := set.Replace(map[IPv4Subnet]IPSetTimeout{{Address: [4]byte{192, 0, 2, 1}, CIDR: 255}: nil}, nil); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	stored, err = set.ListIPv4Subnets()
	if err != nil || len(stored) != len(addresses) {
		t.Fatalf("failed replacement damaged the active set: %d: %v", len(stored), err)
	}
}
