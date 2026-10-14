//go:build integration && linux

package netfilterTools

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"magitrickle/utils/iptables"
)

// Run only inside a disposable network namespace (see CI). Exercises the real
// kernel's ipset, MARK, CONNMARK and iptables-restore, with loopback packets.
func TestRealRouting(t *testing.T) {
	run := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return string(out)
	}
	run("ip", "link", "set", "lo", "up")
	nh, err := New("MT_", "mt_", false, false, 100)
	if err != nil {
		t.Fatal(err)
	}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, ipt := range []*iptables.IPTables{nh.IPTables4, nh.IPTables6} {
		for table, chain := range map[string]string{"mangle": "PREROUTING", "filter": "FORWARD", "nat": "POSTROUTING"} {
			check(ipt.RegisterChainPatch(table, chain))
		}
	}
	check(nh.InitRouting())
	groupSet, subSet := nh.IPSet("group"), nh.IPSet("sub-test")
	check(groupSet.Enable())
	check(subSet.Enable())
	// A DNS-resolved host inside a subscription's broad network, in both families.
	run("ipset", "add", "mt_group_4", "127.0.0.2", "timeout", "0")
	run("ipset", "add", "mt_sub-test_4", "127.0.0.0/8", "timeout", "0")
	run("ipset", "add", "mt_group_6", "::1", "timeout", "0")
	run("ipset", "add", "mt_sub-test_6", "::/1", "timeout", "0")
	group := nh.IPSetToLink("group", "lo", groupSet, false)
	sub := nh.IPSetToLink("sub-test", "lo", subSet, true)
	group.mark, sub.mark = 101, 202
	for _, ipt := range []*iptables.IPTables{nh.IPTables4, nh.IPTables6} {
		check(group.insertIPTablesRules(ipt))
		check(sub.insertIPTablesRules(ipt)) // Previously overwrote the group's mark.
	}
	for _, family := range []struct{ command, address, groupSet string }{
		{"iptables", "127.0.0.2", "mt_group_4"},
		{"ip6tables", "::1", "mt_group_6"},
	} {
		ipt := nh.IPTables4
		if family.command == "ip6tables" {
			ipt = nh.IPTables6
		}
		run(family.command, "-t", "mangle", "-N", "CHECK_MARK")
		verify := func(mark uint32) {
			t.Helper()
			run(family.command, "-t", "mangle", "-F", "OUTPUT")
			run(family.command, "-t", "mangle", "-A", "OUTPUT", "-j", "MT_ROUTE")
			value := fmt.Sprint(mark)
			run(family.command, "-t", "mangle", "-A", "OUTPUT", "-m", "mark", "--mark", value, "-m", "connmark", "--mark", value, "-j", "CHECK_MARK")
			run("ping", "-c", "1", "-W", "2", family.address)
			listing := run(family.command, "-t", "mangle", "-L", "OUTPUT", "-nvx")
			for _, line := range strings.Split(listing, "\n") {
				fields := strings.Fields(line)
				if len(fields) > 2 && fields[2] == "CHECK_MARK" {
					packets, err := strconv.Atoi(fields[0])
					check(err)
					if packets > 0 {
						return
					}
				}
			}
			t.Fatalf("%s: no packet with both packet and connection mark %d\n%s", family.command, mark, listing)
		}
		verify(group.mark)
		check(sub.deleteIPTablesRules(ipt))
		check(sub.insertIPTablesRules(ipt))
		verify(group.mark)
		check(group.deleteIPTablesRules(ipt))
		verify(sub.mark)
		check(group.insertIPTablesRules(ipt))
		verify(group.mark)
		run("ipset", "flush", family.groupSet)
		verify(sub.mark)
	}
	// Real kernels reject deletion of a chain still referenced by another chain.
	check(nh.CleanIPTables())
	for _, ipt := range []*iptables.IPTables{nh.IPTables4, nh.IPTables6} {
		rules, err := ipt.GetCurrentRules()
		check(err)
		for table, chains := range rules {
			for name := range chains {
				if strings.HasPrefix(name, "MT_") {
					t.Fatalf("owned chain survived cleanup: %s/%s", table, name)
				}
			}
		}
		if _, ok := rules["mangle"]["CHECK_MARK"]; !ok {
			t.Fatal("cleanup removed a foreign chain")
		}
	}
	check(groupSet.Disable())
	check(subSet.Disable())
	check(nh.InitRouting())
	check(nh.CleanIPTables())
}
