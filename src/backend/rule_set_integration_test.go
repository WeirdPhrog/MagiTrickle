//go:build integration && linux

package magitrickle

import (
	"net"
	"testing"

	"magitrickle/constant"
	"magitrickle/models"
	"magitrickle/utils/intID"
	"magitrickle/utils/iptables"
	"magitrickle/utils/netfilterTools"
	"magitrickle/utils/recordsCache"
)

// Run in an isolated network namespace. Covers the application's actual Sync,
// including the kernel's canonical /32 and /128 representation of DNS hosts.
func TestRealRuleSetSync(t *testing.T) {
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	nh, err := netfilterTools.New("MT_", "mt_", false, false, 10000)
	check(err)
	for _, ipt := range []*iptables.IPTables{nh.IPTables4, nh.IPTables6} {
		for table, chain := range map[string]string{"mangle": "PREROUTING", "filter": "FORWARD", "nat": "POSTROUTING"} {
			check(ipt.RegisterChainPatch(table, chain))
		}
	}
	check(nh.InitRouting())
	defer nh.CleanIPTables()
	a := &App{config: constant.DefaultAppConfig, nfHelper: nh, recordsCache: recordsCache.New()}
	a.enabled.Store(true)
	ip4 := net.IP{192, 0, 2, 1}
	ip6 := net.ParseIP("2001:db8::1").To16()
	a.recordsCache.AddAddress("example.com", ip4, 120)
	a.recordsCache.AddAddress("example.com", ip6, 120)
	model := &models.Group{ID: intID.ID{1}, Interface: "blackhole", Enable: true, Rules: []*models.Rule{{ID: intID.ID{2}, Type: models.RuleTypeDomain, Rule: "example.com", Enable: true}}}
	check(a.AddGroup(model))
	g := a.userRuleSets[0]
	defer g.Disable()
	host4 := netfilterTools.IPv4Subnet{Address: [4]byte(ip4), CIDR: 32}
	host6 := netfilterTools.IPv6Subnet{Address: [16]byte(ip6), CIDR: 128}
	assertHosts := func(permanent bool) {
		t.Helper()
		v4, err := g.ListIPv4Subnets()
		check(err)
		v6, err := g.ListIPv6Subnets()
		check(err)
		ttl4, ok4 := v4[host4]
		ttl6, ok6 := v6[host6]
		if !ok4 || !ok6 {
			t.Fatalf("DNS host lost: IPv4=%v IPv6=%v", v4, v6)
		}
		if (ttl4 == nil) != permanent || (ttl6 == nil) != permanent {
			t.Fatal("incorrect static/dynamic host timeout")
		}
	}
	for i := 0; i < 3; i++ {
		check(g.Sync())
		assertHosts(false)
	}
	static := *model
	static.Rules = append(append([]*models.Rule(nil), model.Rules...), &models.Rule{ID: intID.ID{3}, Type: models.RuleTypeSubnet, Rule: "192.0.2.1/32", Enable: true}, &models.Rule{ID: intID.ID{4}, Type: models.RuleTypeSubnet6, Rule: "2001:db8::1/128", Enable: true})
	check(a.UpdateGroup(&static))
	ttl := uint32(30)
	check(g.AddIPv4Subnet(host4, &ttl))
	check(g.AddIPv6Subnet(host6, &ttl))
	assertHosts(true)
	check(a.UpdateGroup(model))
	assertHosts(false)
	old := g.Model()
	if err := a.ReplaceGroups([]*models.Group{model, model}); err == nil {
		t.Fatal("duplicate group accepted")
	}
	if !g.Enabled() || g.Model() != old {
		t.Fatal("invalid bulk edit disrupted routing")
	}
	assertHosts(false)
}
