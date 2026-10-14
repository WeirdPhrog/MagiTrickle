//go:build integration && linux

package netfilterTools

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

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
	// The kernel's MASQUERADE target cannot choose a routable source on lo.
	// Bypass only loopback NAT in this harness; mangle/conntrack stay real.
	for _, command := range []string{"iptables", "ip6tables"} {
		run(command, "-t", "nat", "-I", "POSTROUTING", "1", "-o", "lo", "-j", "ACCEPT")
	}
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
	for _, family := range []struct{ command, address, groupSet, subSet string }{
		{"iptables", "127.0.0.2", "mt_group_4", "mt_sub-test_4"},
		{"ip6tables", "::1", "mt_group_6", "mt_sub-test_6"},
	} {
		ipt := nh.IPTables4
		if family.command == "ip6tables" {
			ipt = nh.IPTables6
		}
		run(family.command, "-t", "mangle", "-N", "CHECK_MARK")
		checkCounters := func(want int) {
			t.Helper()
			listing := run(family.command, "-t", "mangle", "-L", "OUTPUT", "-nvx")
			matched := 0
			for _, line := range strings.Split(listing, "\n") {
				fields := strings.Fields(line)
				if len(fields) > 2 && fields[2] == "CHECK_MARK" {
					packets, err := strconv.Atoi(fields[0])
					check(err)
					if packets > 0 {
						matched++
					}
				}
			}
			if matched != want {
				t.Fatalf("%s: %d mark checks matched, want %d\n%s", family.command, matched, want, listing)
			}
		}
		verify := func(mark uint32) {
			t.Helper()
			run(family.command, "-t", "mangle", "-F", "OUTPUT")
			run(family.command, "-t", "mangle", "-A", "OUTPUT", "-j", "MT_ROUTE")
			value := fmt.Sprint(mark)
			run(family.command, "-t", "mangle", "-A", "OUTPUT", "-m", "mark", "--mark", value, "-m", "connmark", "--mark", value, "-j", "CHECK_MARK")
			// A new UDP flow exercises marking without depending on an echo
			// reply through the router's MASQUERADE rules on loopback.
			conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP(family.address), Port: 53535})
			check(err)
			_, err = conn.Write([]byte("routing test"))
			_ = conn.Close()
			check(err)
			checkCounters(1)
		}
		verify(group.mark)
		// Simulate the router firewall flushing our rules, then replay the
		// application's desired state through the same Commit hook it uses.
		for _, chain := range []string{"MT_ROUTE", "MT_SUBS", "MT_GROUPS", group.chainName, sub.chainName} {
			run(family.command, "-t", "mangle", "-F", chain)
		}
		run(family.command, "-t", "mangle", "-D", "PREROUTING", "-j", "MT_ROUTE")
		check(ipt.Commit())
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

		// Keep one bidirectional UDP flow open while DNS/set membership changes.
		// Re-evaluate original packets, preserve reply marks, and never save a
		// foreign packet mark into conntrack when no rule set matches.
		server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(family.address)})
		check(err)
		defer server.Close()
		client, err := net.DialUDP("udp", nil, server.LocalAddr().(*net.UDPAddr))
		check(err)
		defer client.Close()
		verifyFlow := func(mark, connmark uint32) {
			t.Helper()
			run(family.command, "-t", "mangle", "-F", "OUTPUT")
			run(family.command, "-t", "mangle", "-A", "OUTPUT", "-j", "MARK", "--set-mark", "77")
			run(family.command, "-t", "mangle", "-A", "OUTPUT", "-j", "MT_ROUTE")
			for direction, value := range map[string]uint32{"ORIGINAL": mark, "REPLY": 77} {
				run(family.command, "-t", "mangle", "-A", "OUTPUT", "-m", "conntrack", "--ctdir", direction,
					"-m", "mark", "--mark", fmt.Sprint(value), "-m", "connmark", "--mark", fmt.Sprint(connmark), "-j", "CHECK_MARK")
			}
			check(server.SetDeadline(time.Now().Add(5 * time.Second)))
			check(client.SetDeadline(time.Now().Add(5 * time.Second)))
			_, err := client.Write([]byte("routing flow"))
			check(err)
			var buf [64]byte
			n, peer, err := server.ReadFromUDP(buf[:])
			check(err)
			_, err = server.WriteToUDP(buf[:n], peer)
			check(err)
			_, err = client.Read(buf[:])
			check(err)
			checkCounters(2)
		}
		verifyFlow(sub.mark, sub.mark)
		run("ipset", "add", family.groupSet, family.address, "timeout", "0")
		verifyFlow(group.mark, group.mark)
		run("ipset", "flush", family.groupSet)
		verifyFlow(sub.mark, sub.mark)
		run("ipset", "flush", family.subSet)
		verifyFlow(77, sub.mark)
		check(client.Close())
		check(server.Close())
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
