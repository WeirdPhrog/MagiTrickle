package recordsCache

import (
	"net"
	"slices"
	"testing"
	"time"
)

func TestBindingTargetsExpiryAndCycles(t *testing.T) {
	r := New()
	r.AddBinding("OWNER", "first", 120)
	r.AddBinding("owner", "second", 120)
	r.AddBinding("first", "owner", 120)
	r.AddAlias("alias", "owner", 120)
	r.AddAddress("first", net.IPv4(192, 0, 2, 1).To4(), 120)
	r.AddAddress("second", net.IPv4(192, 0, 2, 2).To4(), 120)
	if got := r.GetAddresses("alias"); len(got) != 2 {
		t.Fatalf("multiple targets/cycle: got %v", got)
	}
	if got := r.GetAliases("second"); !slices.Contains(got, "owner") || !slices.Contains(got, "alias") {
		t.Fatalf("missing reverse bindings: %v", got)
	}
	r.bindings["owner"]["second"] = time.Now().Add(-time.Second)
	r.reverseBindings["second"]["owner"] = r.bindings["owner"]["second"]
	r.cleanupRecords()
	if len(r.GetAddresses("owner")) != 1 || len(r.reverseBindings["second"]) != 0 {
		t.Fatal("expired service binding survived cleanup")
	}
}

func TestNewAnswerDoesNotShortenEarlierClientTTL(t *testing.T) {
	r := New()
	ip := net.IPv4(192, 0, 2, 1).To4()
	r.AddAddress("example", ip, 120)
	deadline := r.GetAddresses("example")[0].Deadline
	r.AddAddress("example", ip, 1)
	if got := r.GetAddresses("example")[0].Deadline; !got.Equal(deadline) {
		t.Fatal("short answer removed the lifetime of an earlier cached answer")
	}
	r.AddAlias("alias", "example", 120)
	aliasDeadline := r.aliases["alias"].Deadline
	r.AddAlias("alias", "example", 1)
	if !r.aliases["alias"].Deadline.Equal(aliasDeadline) {
		t.Fatal("short answer removed the earlier CNAME lifetime")
	}
}
