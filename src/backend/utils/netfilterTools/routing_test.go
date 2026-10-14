//go:build testing

package netfilterTools

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"magitrickle/utils/iptables"
)

func TestRoutingPriority(t *testing.T) {
	for _, proto := range []iptables.Protocol{iptables.ProtocolIPv4, iptables.ProtocolIPv6} {
		for _, subscriptionFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("family=%d/subscriptionFirst=%t", proto, subscriptionFirst), func(t *testing.T) {
				fake := iptables.NewFakeIPTables(proto)
				ipt := iptables.NewIPTables(fake)
				nh := &Helper{ChainPrefix: "MT_", IpsetPrefix: "mt_"}
				if proto == iptables.ProtocolIPv4 {
					nh.IPTables4 = ipt
				} else {
					nh.IPTables6 = ipt
				}
				for table, chain := range map[string]string{"mangle": "PREROUTING", "filter": "FORWARD", "nat": "POSTROUTING"} {
					must(t, ipt.RegisterChainPatch(table, chain))
				}
				must(t, nh.InitRouting())
				group := nh.IPSetToLink("group", "vpn-group", nh.IPSet("group"), false)
				sub := nh.IPSetToLink("sub-test", "vpn-sub", nh.IPSet("sub-test"), true)
				group.mark, sub.mark = 101, 202
				order := []*IPSetToLink{group, sub}
				if subscriptionFirst {
					order = []*IPSetToLink{sub, group}
				}
				for _, r := range order {
					must(t, r.insertIPTablesRules(ipt))
				}
				suffix := "_4"
				if proto == iptables.ProtocolIPv6 {
					suffix = "_6"
				}
				sets := map[string]bool{"mt_group" + suffix: true, "mt_sub-test" + suffix: true}
				check := func(want uint32) {
					t.Helper()
					mark, connmark := routePacket(t, fake, sets, false, 77)
					if mark != want || connmark != want {
						t.Fatalf("packet/connection marks = %d/%d, want %d", mark, connmark, want)
					}
				}
				check(group.mark)
				// Subscription refresh and group interface edits must not change tiers.
				for _, r := range []*IPSetToLink{sub, group, sub} {
					must(t, r.deleteIPTablesRules(ipt))
					must(t, r.insertIPTablesRules(ipt))
					check(group.mark)
				}
				must(t, ipt.Commit()) // Netfilter hook / firewall reconstruction.
				check(group.mark)
				sets["mt_group"+suffix] = false // DNS address expires or stops matching.
				check(sub.mark)
				sets["mt_group"+suffix] = true
				must(t, group.deleteIPTablesRules(ipt)) // Disabled/deleted group.
				check(sub.mark)
				must(t, group.insertIPTablesRules(ipt))
				sets["mt_sub-test"+suffix] = false
				check(group.mark)
				sets["mt_group"+suffix] = false
				check(77) // Preserve unrelated marks when neither tier matches.
				sets["mt_group"+suffix], sets["mt_sub-test"+suffix] = true, true
				if mark, connmark := routePacket(t, fake, sets, true, 88); mark != 88 || connmark != 88 {
					t.Fatal("reply traffic was re-marked")
				}
				if got := fake.GetRules("mangle", "MT_ROUTE"); !reflect.DeepEqual(got, [][]string{{"-j", "MT_SUBS"}, {"-j", "MT_GROUPS"}}) {
					t.Fatalf("unexpected tier order: %v", got)
				}
				must(t, nh.CleanIPTables())
				for _, name := range []string{"MT_ROUTE", "MT_SUBS", "MT_GROUPS", group.chainName, sub.chainName} {
					if fake.ChainExists("mangle", name) {
						t.Fatalf("chain %s survived cleanup", name)
					}
				}
				must(t, nh.InitRouting())
				must(t, sub.insertIPTablesRules(ipt))
				must(t, group.insertIPTablesRules(ipt))
				check(group.mark)
			})
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Interpret only the generated mangle rules, failing on unexpected targets.
func routePacket(t *testing.T, fake *iptables.FakeIPTables, sets map[string]bool, reply bool, initial uint32) (uint32, uint32) {
	t.Helper()
	mark, connmark := initial, initial
	var walk func(string)
	walk = func(chain string) {
		for _, rule := range fake.GetRules("mangle", chain) {
			arg := func(flag string) string {
				for i := 0; i+1 < len(rule); i++ {
					if rule[i] == flag {
						return rule[i+1]
					}
				}
				return ""
			}
			if arg("--ctdir") == "REPLY" && !reply {
				continue
			}
			if set := arg("--match-set"); set != "" && !sets[set] {
				continue
			}
			switch target := arg("-j"); target {
			case "RETURN":
				return
			case "MARK":
				value, err := strconv.ParseUint(arg("--set-mark"), 10, 32)
				must(t, err)
				mark = uint32(value)
			case "CONNMARK":
				connmark = mark
			default:
				if !fake.ChainExists("mangle", target) {
					t.Fatalf("unknown target %q", target)
				}
				walk(target)
			}
		}
	}
	walk("PREROUTING")
	return mark, connmark
}
