package magitrickle

import (
	"net"
	"testing"

	"magitrickle/constant"
	"magitrickle/utils/recordsCache"

	"github.com/miekg/dns"
)

func TestServiceBindingAddressesAndIPv6Filtering(t *testing.T) {
	a := &App{config: constant.DefaultAppConfig, recordsCache: recordsCache.New()}
	msg := dns.Msg{Answer: []dns.RR{}}
	for _, text := range []string{
		"www.example. 120 IN HTTPS 1 first.example. ipv4hint=192.0.2.10 ipv6hint=2001:db8::10",
		"www.example. 120 IN HTTPS 2 second.example.",
		"service.example. 120 IN SVCB 0 www.example.",
		"alias.example. 120 IN CNAME service.example.",
	} {
		rr, err := dns.NewRR(text)
		if err != nil {
			t.Fatal(err)
		}
		msg.Answer = append(msg.Answer, rr)
	}
	for _, text := range []string{"first.example. 120 IN A 192.0.2.11", "second.example. 120 IN AAAA 2001:db8::12"} {
		rr, _ := dns.NewRR(text)
		msg.Extra = append(msg.Extra, rr)
	}
	response, err := a.dnsResponseHook(nil, dns.Msg{}, msg, "udp")
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"www.example", "service.example", "alias.example"} {
		addresses := a.recordsCache.GetAddresses(owner)
		if len(addresses) != 4 {
			t.Fatalf("%s: got %d addresses, want hints and both targets", owner, len(addresses))
		}
	}
	if len(response.Extra) != 1 || len(response.Answer[0].(*dns.HTTPS).Value) != 1 {
		t.Fatalf("IPv6 address/hint bypassed filtering: %s", response)
	}
	if len(msg.Answer[0].(*dns.HTTPS).Value) != 2 {
		t.Fatal("filter changed the original response")
	}
	mandatory, _ := dns.NewRR("www.example. 60 IN HTTPS 1 . mandatory=ipv6hint ipv6hint=2001:db8::10")
	if got, _ := withoutIPv6([]dns.RR{mandatory}); len(got) != 0 {
		t.Fatal("record retained after removing a mandatory parameter")
	}
	a.config.DNSProxy.DisableDropAAAA = true
	response, err = a.dnsResponseHook(nil, dns.Msg{}, msg, "udp")
	if err != nil || response != nil {
		t.Fatal("IPv6-enabled mode should preserve the original response")
	}
}

func TestDNSAliasesLearnedBeforeAddresses(t *testing.T) {
	a := &App{config: constant.DefaultAppConfig, recordsCache: recordsCache.New()}
	address, _ := dns.NewRR("target.example. 60 IN A 192.0.2.1")
	alias, _ := dns.NewRR("owner.example. 60 IN CNAME target.example.")
	if err := a.handleMessage(dns.Msg{Answer: []dns.RR{address, alias}}, nil, "udp"); err != nil {
		t.Fatal(err)
	}
	addresses := a.recordsCache.GetAddresses("owner.example")
	if len(addresses) != 1 || !addresses[0].Address.Equal(net.ParseIP("192.0.2.1")) {
		t.Fatal("out-of-order CNAME chain lost its address")
	}
}
