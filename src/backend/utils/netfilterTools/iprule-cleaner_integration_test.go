//go:build integration && linux

package netfilterTools

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"magitrickle/utils/iptables"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
)

// Run only in the disposable namespace used by the real routing CI tests. What
// a killed instance left goes on the next start, and nothing that merely looks
// similar goes with it.
func TestRealRoutingStaleCleanup(t *testing.T) {
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
	tableRoutes := func(table int) []netlink.Route {
		t.Helper()
		routes, err := netlink.RouteListFiltered(nl.FAMILY_ALL, &netlink.Route{Table: table}, netlink.RT_FILTER_TABLE)
		check(err)
		return routes
	}
	run("ip", "link", "add", "mt-stale", "type", "dummy")
	defer exec.Command("ip", "link", "del", "mt-stale").Run()
	run("ip", "link", "set", "mt-stale", "up")
	run("ip", "addr", "add", "192.0.2.2/24", "dev", "mt-stale")
	run("ip", "-6", "addr", "add", "2001:db8:5::2/64", "dev", "mt-stale", "nodad")
	run("ip", "route", "add", "default", "via", "192.0.2.1", "dev", "mt-stale")
	run("ip", "-6", "route", "add", "default", "via", "2001:db8:5::1", "dev", "mt-stale")

	nh, err := New("CL_", "cl_", false, false, 40000)
	check(err)
	for _, ipt := range []*iptables.IPTables{nh.IPTables4, nh.IPTables6} {
		for table, chain := range map[string]string{"mangle": "PREROUTING", "filter": "FORWARD", "nat": "POSTROUTING"} {
			check(ipt.RegisterChainPatch(table, chain))
		}
	}
	check(nh.InitRouting())

	// A killed instance never disables its rule sets.
	set := nh.IPSet("stale")
	check(set.Enable())
	link := nh.IPSetToLink("stale", "mt-stale", set, false)
	check(link.Enable())
	staleTable := strconv.Itoa(link.table)

	// Lookalikes: another metric above StartIdx, and our metric below it.
	foreignTables := map[int]string{41000: "99", 39000: strconv.Itoa(blackholeRouteMetric)}
	for table, metric := range foreignTables {
		for _, family := range []string{"-4", "-6"} {
			run("ip", family, "route", "add", "blackhole", "default", "metric", metric, "table", strconv.Itoa(table))
			run("ip", family, "rule", "add", "fwmark", strconv.Itoa(table), "lookup", strconv.Itoa(table))
		}
	}
	run("ipset", "create", "other_4", "hash:ip")

	assertState := func(stale bool) {
		t.Helper()
		for _, family := range []string{"-4", "-6"} {
			rules := run("ip", family, "rule", "show")
			if strings.Contains(rules, "lookup "+staleTable) != stale {
				t.Fatalf("family %s: stale rule expected %v:\n%s", family, stale, rules)
			}
			for table := range foreignTables {
				if !strings.Contains(rules, "lookup "+strconv.Itoa(table)) {
					t.Fatalf("family %s: foreign rule %d removed:\n%s", family, table, rules)
				}
			}
		}
		want := 0
		if stale {
			want = 4 // A blackhole and a device route in each family.
		}
		if routes := tableRoutes(link.table); len(routes) != want {
			t.Fatalf("stale table: want %d routes, got %+v", want, routes)
		}
		for table := range foreignTables {
			if routes := tableRoutes(table); len(routes) != 2 {
				t.Fatalf("foreign table %d changed: %+v", table, routes)
			}
		}
		sets := run("ipset", "list", "-n")
		if strings.Contains(sets, "cl_stale_4") != stale || strings.Contains(sets, "cl_stale_6") != stale {
			t.Fatalf("stale ipsets expected %v:\n%s", stale, sets)
		}
		if !strings.Contains(sets, "other_4") {
			t.Fatalf("foreign ipset removed:\n%s", sets)
		}
	}
	assertState(true)
	// Twice: a start after a clean stop finds nothing, and that is no error.
	for range 2 {
		check(nh.CleanIPTables())
		check(nh.CleanIPSets())
		check(nh.CleanIPRules())
		assertState(false)
	}
}
