package netfilterTools

import (
	"fmt"

	"magitrickle/utils/iptables"
)

// InitRouting installs fixed routing tiers. MARK is not terminal: groups run
// last so an explicit group overrides subscriptions, regardless of when either
// rule set is enabled or refreshed. Ordering within each tier is unchanged.
// Call once after CleanIPTables, before enabling any rule sets.
func (nh *Helper) InitRouting() error {
	for _, ipt := range []*iptables.IPTables{nh.IPTables4, nh.IPTables6} {
		if ipt == nil {
			continue
		}
		routing := nh.ChainPrefix + "ROUTE"
		for _, suffix := range []string{"ROUTE", "SUBS", "GROUPS"} {
			if err := ipt.RegisterChainOverride("mangle", nh.ChainPrefix+suffix); err != nil {
				return err
			}
		}
		// Reply packets keep their existing marks. Check once for all rule sets.
		if err := ipt.Append("mangle", routing, "-m", "conntrack", "--ctdir", "REPLY", "-j", "RETURN"); err != nil {
			return err
		}
		for _, suffix := range []string{"SUBS", "GROUPS"} {
			if err := ipt.Append("mangle", routing, "-j", nh.ChainPrefix+suffix); err != nil {
				return err
			}
		}
		if err := ipt.Append("mangle", "PREROUTING", "-j", routing); err != nil {
			return err
		}
		if err := ipt.Commit(); err != nil {
			return fmt.Errorf("failed to initialize routing chains: %w", err)
		}
	}
	return nil
}
