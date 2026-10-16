//go:build integration && linux

package magitrickle

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"magitrickle/constant"
	"magitrickle/models"
	"magitrickle/utils/intID"
	"magitrickle/utils/iptables"
	"magitrickle/utils/netfilterTools"
	"magitrickle/utils/recordsCache"
)

type probeIPTables struct {
	iptables.Executable
	probe func()
}

func (p *probeIPTables) Save() ([]byte, error) {
	p.probe()
	return p.Executable.Save()
}

func (p *probeIPTables) Restore(data []byte) error {
	p.probe()
	err := p.Executable.Restore(data)
	p.probe()
	return err
}

// Probe real egress between every firewall transaction as well as after edits.
// The main table has a working WAN default, so a missing policy route or set
// produces an observable WAN packet instead of being hidden by no route.
func TestRealRuleSetSyncUpdates(t *testing.T) {
	run := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return string(out)
	}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	run("ip", "link", "set", "lo", "up")
	for i, iface := range []string{"mtwan", "mtvpn", "mtother"} {
		run("ip", "link", "add", iface, "type", "dummy")
		defer exec.Command("ip", "link", "del", iface).Run()
		run("ip", "link", "set", iface, "up")
		run("ip", "addr", "add", fmt.Sprintf("192.0.%d.2/24", i+2), "dev", iface)
		run("ip", "-6", "addr", "add", fmt.Sprintf("2001:db8:%d::2/64", i+2), "dev", iface, "nodad")
	}
	run("ip", "route", "add", "default", "dev", "mtwan")
	run("ip", "-6", "route", "add", "default", "dev", "mtwan")
	families := []struct{ command, network, address string }{
		{"iptables", "udp4", "198.18.0.1"}, {"ip6tables", "udp6", "2001:db8:ffff::1"},
	}
	for _, f := range families {
		// Counters live in separate chains so app commits never reset them.
		for _, iface := range []string{"mtwan", "mtvpn", "mtother"} {
			run(f.command, "-t", "mangle", "-N", "EGRESS_"+iface)
			run(f.command, "-t", "mangle", "-A", "EGRESS_"+iface, "-j", "DROP")
			run(f.command, "-t", "mangle", "-A", "POSTROUTING", "-d", f.address, "-o", iface, "-j", "EGRESS_"+iface)
		}
	}
	probe := func() {
		for _, f := range families {
			conn, err := net.DialUDP(f.network, nil, &net.UDPAddr{IP: net.ParseIP(f.address), Port: 53535})
			if err == nil {
				_, _ = conn.Write([]byte("routing probe"))
				conn.Close()
			}
		}
	}
	probing := false
	wrap := func(e iptables.Executable) *iptables.IPTables {
		return iptables.NewIPTables(&probeIPTables{Executable: e, probe: func() {
			if probing {
				probe()
			}
		}})
	}
	nh := &netfilterTools.Helper{ChainPrefix: "UP_", IpsetPrefix: "up_", StartIdx: 20000,
		IPTables4: wrap(iptables.NewRealIPTables()), IPTables6: wrap(iptables.NewRealIP6Tables())}
	for _, ipt := range []*iptables.IPTables{nh.IPTables4, nh.IPTables6} {
		for table, chain := range map[string]string{"mangle": "PREROUTING", "filter": "FORWARD", "nat": "POSTROUTING"} {
			check(ipt.RegisterChainPatch(table, chain))
		}
	}
	check(nh.InitRouting())
	defer nh.CleanIPTables()
	for _, f := range families {
		run(f.command, "-t", "mangle", "-A", "OUTPUT", "-j", "UP_ROUTE")
	}
	a := &App{config: constant.DefaultAppConfig, nfHelper: nh, recordsCache: recordsCache.New()}
	a.enabled.Store(true)
	defer func() {
		probing = false
		disableRuleSets(a.userRuleSets)
		disableRuleSets(a.subscriptionRuleSets)
	}()
	sub := &models.Subscription{ID: intID.ID{11}, Interface: "mtvpn", Enable: true, Rules: []*models.SubscriptionRule{
		{ID: intID.ID{12}, Type: models.RuleTypeSubnet, Rule: "198.18.0.0/15", Enable: true},
		{ID: intID.ID{13}, Type: models.RuleTypeSubnet6, Rule: "2001:db8::/32", Enable: true},
	}}
	check(a.AddSubscription(sub))
	original := a.subscriptionRuleSets[0]
	oldSet, oldLink := original.ipset, original.ipsetToLink
	count := func(command, iface string) uint64 {
		t.Helper()
		out := run(command, "-t", "mangle", "-L", "EGRESS_"+iface, "-nvx")
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) > 2 && f[2] == "DROP" {
				n, err := strconv.ParseUint(f[0], 10, 64)
				check(err)
				return n
			}
		}
		t.Fatalf("missing counter: %s", out)
		return 0
	}
	assertEgress := func(iface string) {
		t.Helper()
		for _, f := range families {
			before := count(f.command, iface)
			probe()
			if count(f.command, iface) <= before {
				t.Fatalf("%s: packet did not use %s", f.network, iface)
			}
			if n := count(f.command, "mtwan"); n != 0 {
				t.Fatalf("%s: %d packets leaked to WAN", f.network, n)
			}
		}
	}
	probing = true
	assertEgress("mtvpn")
	check(a.AddSubscription(&models.Subscription{ID: intID.ID{14}, Interface: "mtother", Enable: true}))
	updated := *sub
	updated.Rules = append(append([]*models.SubscriptionRule(nil), sub.Rules...), &models.SubscriptionRule{ID: intID.ID{15}, Type: models.RuleTypeSubnet, Rule: "203.0.113.0/24", Enable: true})
	check(a.ReplaceSubscriptions([]*models.Subscription{&updated, a.subscriptions[1]}))
	if a.subscriptionRuleSets[0] != original || original.ipset != oldSet || original.ipsetToLink != oldLink {
		t.Fatal("subscription refresh destroyed live routing")
	}
	assertEgress("mtvpn")
	group := &models.Group{ID: intID.ID{21}, Interface: "mtother", Enable: true, Rules: []*models.Rule{
		{ID: intID.ID{22}, Type: models.RuleTypeSubnet, Rule: "198.18.0.1/32", Enable: true},
		{ID: intID.ID{23}, Type: models.RuleTypeSubnet6, Rule: "2001:db8:ffff::1/128", Enable: true},
	}}
	check(a.AddGroup(group))
	g := a.userRuleSets[0]
	oldSet, oldLink = g.ipset, g.ipsetToLink
	assertEgress("mtother")
	changed := *group
	changed.Interface = "mtvpn"
	check(a.UpdateGroup(&changed))
	if g.ipset != oldSet || g.ipsetToLink != oldLink {
		t.Fatal("interface edit destroyed live routing")
	}
	assertEgress("mtvpn")
	check(a.ReplaceGroups([]*models.Group{group}))
	assertEgress("mtother")
	// A bad replacement must preserve both old address families.
	if err := g.ipset.Replace(map[netfilterTools.IPv4Subnet]netfilterTools.IPSetTimeout{{Address: [4]byte{198, 18, 0, 1}, CIDR: 32}: nil}, map[netfilterTools.IPv6Subnet]netfilterTools.IPSetTimeout{{CIDR: 255}: nil}); err == nil {
		t.Fatal("invalid replacement unexpectedly succeeded")
	}
	assertEgress("mtother")
	// Missing/down/deleted VPN and explicit blackhole must not fall through
	// to the subscription VPN or the working system WAN default.
	assertBlocked := func() {
		t.Helper()
		for _, f := range families {
			for _, iface := range []string{"mtwan", "mtvpn", "mtother"} {
				before := count(f.command, iface)
				probe()
				if count(f.command, iface) != before {
					t.Fatalf("%s: blocked traffic escaped through %s", f.network, iface)
				}
			}
		}
	}
	for _, iface := range []string{"missing-vpn", "blackhole", "mtother"} {
		changed.Interface = iface
		check(a.UpdateGroup(&changed))
		if iface == "mtother" {
			run("ip", "link", "set", "mtother", "down")
		}
		assertBlocked()
	}
	run("ip", "link", "del", "mtother")
	assertBlocked()
}
