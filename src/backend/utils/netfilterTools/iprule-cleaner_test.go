package netfilterTools

import (
	"net"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestHasOwnBlackholeRoute(t *testing.T) {
	_, anyV4, _ := net.ParseCIDR("0.0.0.0/0")
	_, anyV6, _ := net.ParseCIDR("::/0")
	_, lan, _ := net.ParseCIDR("192.168.1.0/24")

	tests := map[string]struct {
		routes []netlink.Route
		want   bool
	}{
		"empty table": {nil, false},
		"blackhole default v4": {[]netlink.Route{
			{Type: unix.RTN_BLACKHOLE, Priority: blackholeRouteMetric, Dst: anyV4},
		}, true},
		"blackhole default v6": {[]netlink.Route{
			{Type: unix.RTN_BLACKHOLE, Priority: blackholeRouteMetric, Dst: anyV6},
		}, true},
		"default route listed without a destination": {[]netlink.Route{
			{Type: unix.RTN_BLACKHOLE, Priority: blackholeRouteMetric},
		}, true},
		"found among other routes": {[]netlink.Route{
			{Type: unix.RTN_UNICAST, Priority: deviceRouteMetric},
			{Type: unix.RTN_BLACKHOLE, Priority: blackholeRouteMetric, Dst: anyV4},
		}, true},
		"other metric": {[]netlink.Route{
			{Type: unix.RTN_BLACKHOLE, Priority: blackholeRouteMetric + 1, Dst: anyV4},
		}, false},
		"not a blackhole": {[]netlink.Route{
			{Type: unix.RTN_UNICAST, Priority: blackholeRouteMetric, Dst: anyV4},
		}, false},
		"blackhole of a specific network": {[]netlink.Route{
			{Type: unix.RTN_BLACKHOLE, Priority: blackholeRouteMetric, Dst: lan},
		}, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := hasOwnBlackholeRoute(tc.routes); got != tc.want {
				t.Errorf("hasOwnBlackholeRoute = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewRejectsShortPrefixes(t *testing.T) {
	tests := []struct {
		name         string
		chain, ipset string
		wantErr      bool
	}{
		{"defaults", "MT_", "mt_", false},
		{"longer", "MAGI_", "magi_", false},
		{"empty chain prefix", "", "mt_", true},
		{"empty ipset prefix", "MT_", "", true},
		{"two characters in chain prefix", "MT", "mt_", true},
		{"two characters in ipset prefix", "MT_", "mt", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, err := New(tc.chain, tc.ipset, true, true, 1000)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && h != nil {
				t.Error("helper returned together with an error")
			}
		})
	}
}
