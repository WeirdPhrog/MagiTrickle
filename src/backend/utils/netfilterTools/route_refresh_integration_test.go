//go:build integration && linux

package netfilterTools

import (
	"net"
	"os/exec"
	"testing"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

// Run only in the disposable namespace used by the real routing CI tests.
func TestRealRoutingRouteRefresh(t *testing.T) {
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
			t.Fatalf("ip %v: %v\n%s", args, err, out)
		}
	}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	createInterface := func() netlink.Link {
		t.Helper()
		run("link", "add", "mt-refresh", "type", "dummy")
		run("link", "set", "mt-refresh", "up")
		run("addr", "add", "192.0.2.2/24", "dev", "mt-refresh")
		run("-6", "addr", "add", "2001:db8:4::2/64", "dev", "mt-refresh", "nodad")
		run("route", "add", "default", "via", "192.0.2.1", "dev", "mt-refresh")
		run("-6", "route", "add", "default", "via", "2001:db8:4::1", "dev", "mt-refresh")
		iface, err := netlink.LinkByName("mt-refresh")
		check(err)
		return iface
	}
	iface := createInterface()
	defer exec.Command("ip", "link", "del", "mt-refresh").Run()
	routing := &IPSetToLink{ifaceName: "mt-refresh", table: 12345}
	families := []struct {
		family int
		gw     string
		bits   int
	}{
		{nl.FAMILY_V4, "192.0.2.1", 32},
		{nl.FAMILY_V6, "2001:db8:4::1", 128},
	}
	current := make([]*netlink.Route, len(families))
	assertRoute := func(family, index int, gateway net.IP) {
		t.Helper()
		routes, err := netlink.RouteListFiltered(family, &netlink.Route{Table: routing.table}, netlink.RT_FILTER_TABLE)
		check(err)
		active, blackhole := 0, 0
		for _, route := range routes {
			if route.Type == unix.RTN_BLACKHOLE && route.Priority == 20 {
				blackhole++
			}
			if route.Priority == 10 && route.LinkIndex == index && route.Gw.Equal(gateway) {
				active++
			}
		}
		if active != 1 || blackhole != 1 {
			t.Fatalf("family %d: expected active route and blackhole, got %+v", family, routes)
		}
	}
	for i, family := range families {
		check(netlink.RouteAdd(&netlink.Route{Table: routing.table, Family: family.family, Type: unix.RTN_BLACKHOLE, Priority: 20, Dst: &net.IPNet{IP: make(net.IP, family.bits/8), Mask: net.CIDRMask(0, family.bits)}}))
		var err error
		current[i], err = routing.updateIfaceRoute(iface, family.family, nil)
		check(err)
		assertRoute(family.family, iface.Attrs().Index, net.ParseIP(family.gw))
		// The cached route still exists in Go, but the kernel has removed it.
		check(netlink.RouteDel(current[i]))
		current[i], err = routing.updateIfaceRoute(iface, family.family, current[i])
		check(err)
		assertRoute(family.family, iface.Attrs().Index, net.ParseIP(family.gw))
	}
	oldIndex := iface.Attrs().Index
	run("link", "del", "mt-refresh")
	iface = createInterface()
	if iface.Attrs().Index == oldIndex {
		t.Fatal("test interface did not receive a new index")
	}
	for i, family := range families {
		var err error
		current[i], err = routing.updateIfaceRoute(iface, family.family, current[i])
		check(err)
		assertRoute(family.family, iface.Attrs().Index, net.ParseIP(family.gw))
	}
}
