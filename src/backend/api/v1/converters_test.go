package v1

import (
	"encoding/binary"
	"fmt"
	"testing"

	"magitrickle/api/v1/types"
	"magitrickle/models"
	"magitrickle/utils/intID"
)

func bulkRuleFixture(count int) (*models.Group, types.GroupReq, *models.Subscription, types.SubscriptionReq) {
	group := &models.Group{ID: intID.ID{1}, Rules: make([]*models.Rule, count)}
	subscription := &models.Subscription{ID: intID.ID{2}, Rules: make([]*models.SubscriptionRule, count), LastCheck: 123, LastUpdate: 100}
	groupRules := make([]types.RuleReq, count)
	subscriptionRules := make([]types.SubscriptionRuleReq, count)
	for i := 0; i < count; i++ {
		var id intID.ID
		binary.BigEndian.PutUint32(id[:], uint32(i+1))
		group.Rules[i] = &models.Rule{ID: id, Type: models.RuleTypeSubnet, Rule: "192.0.2.0/24", Enable: true}
		subscription.Rules[i] = &models.SubscriptionRule{ID: id, Type: models.RuleTypeSubnet, Rule: "192.0.2.0/24", Enable: true}
		// Reverse the incoming order, and change content and enabled state.
		groupRules[count-i-1] = types.RuleReq{ID: &id, Type: models.RuleTypeDomain, Rule: "example.com", Enable: false}
		subscriptionRules[count-i-1] = types.SubscriptionRuleReq{ID: &id, Type: models.RuleTypeDomain, Rule: "example.com", Enable: false}
	}
	return group, types.GroupReq{ID: &group.ID, RulesReq: types.RulesReq{Rules: &groupRules}}, subscription, types.SubscriptionReq{ID: &subscription.ID, URL: "https://example.com/list", SubscriptionRulesReq: types.SubscriptionRulesReq{Rules: &subscriptionRules}}
}

func TestBulkRuleConversionKeepsIDsOrderAndOriginalSnapshots(t *testing.T) {
	const count = 10000
	group, groupReq, subscription, subscriptionReq := bulkRuleFixture(count)
	nextGroup, err := GroupFromReq(groupReq, group)
	if err != nil {
		t.Fatal(err)
	}
	nextSubscription, err := SubscriptionFromReq(subscriptionReq, subscription)
	if err != nil {
		t.Fatal(err)
	}
	if len(nextGroup.Rules) != count || len(nextSubscription.Rules) != count || nextSubscription.LastCheck != subscription.LastCheck || nextSubscription.LastUpdate != subscription.LastUpdate {
		t.Fatal("rule count or subscription timestamps changed")
	}
	for i := 0; i < count; i++ {
		oldGroupRule, oldSubscriptionRule := group.Rules[count-i-1], subscription.Rules[count-i-1]
		g, s := nextGroup.Rules[i], nextSubscription.Rules[i]
		if g.ID != oldGroupRule.ID || s.ID != oldSubscriptionRule.ID || g.Enable || s.Enable || g.Type != models.RuleTypeDomain || s.Type != models.RuleTypeDomain || g.Rule != "example.com" || s.Rule != "example.com" {
			t.Fatalf("rule %d lost its ID, position, or edited values", i)
		}
		if !oldGroupRule.Enable || !oldSubscriptionRule.Enable || oldGroupRule.Rule != "192.0.2.0/24" || oldSubscriptionRule.Rule != "192.0.2.0/24" {
			t.Fatal("conversion mutated an existing snapshot")
		}
	}
}

func TestNewRulesStillReceiveServerAssignedIDs(t *testing.T) {
	unknown := intID.ID{9}
	for _, id := range []*intID.ID{nil, &unknown} {
		g, err := RuleFromReq(types.RuleReq{ID: id, Rule: "example.com", Type: models.RuleTypeDomain}, nil)
		if err != nil || g.ID.IsZero() || g.ID == unknown {
			t.Fatalf("new group rule ID: %v, %v", g, err)
		}
		s, err := SubscriptionRuleFromReq(types.SubscriptionRuleReq{ID: id, Rule: "example.com", Type: models.RuleTypeDomain}, nil)
		if err != nil || s.ID.IsZero() || s.ID == unknown {
			t.Fatalf("new subscription rule ID: %v, %v", s, err)
		}
	}
}

func BenchmarkBulkRuleConversion(b *testing.B) {
	for _, count := range []int{1000, 100000} {
		group, groupReq, subscription, subscriptionReq := bulkRuleFixture(count)
		b.Run(fmt.Sprintf("group/rules=%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				if _, err := GroupFromReq(groupReq, group); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("subscription/rules=%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				if _, err := SubscriptionFromReq(subscriptionReq, subscription); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
