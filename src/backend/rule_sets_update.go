package magitrickle

import (
	"errors"
	"fmt"

	"magitrickle/models"
	"magitrickle/rulesets"
	"magitrickle/utils/intID"
)

// Caller holds stateMu. Reuse live sets by ID, prepare additions first and
// remove obsolete sets last. Unrelated subscriptions never lose their routes.
func (a *App) reconcileRuleSets(previous []*RuleSet, specs []rulesets.Spec, subscription bool) ([]*RuleSet, error) {
	existing := make(map[intID.ID]*RuleSet, len(previous))
	oldModels := make(map[*RuleSet]*models.Group, len(previous))
	for _, g := range previous {
		existing[g.IDValue()] = g
		oldModels[g] = g.Model()
	}
	seen := make(map[intID.ID]bool, len(specs))
	next := make([]*RuleSet, 0, len(specs))
	for _, spec := range specs {
		if seen[spec.ID] {
			return nil, fmt.Errorf("duplicate rule set %s", spec.ID)
		}
		seen[spec.ID] = true
		g := existing[spec.ID]
		if g == nil {
			var err error
			g, err = newRuleSet(spec, a)
			if err != nil {
				return nil, err
			}
		}
		next = append(next, g)
	}
	order := func(sets []*RuleSet) error {
		if !a.enabled.Load() {
			return nil
		}
		var keys []string
		for _, g := range sets {
			if g.ConfiguredEnabled() {
				keys = append(keys, g.RuntimeKey())
			}
		}
		return a.nfHelper.SetRoutingOrder(keys, subscription)
	}
	rollback := func(cause error) ([]*RuleSet, error) {
		errs := []error{cause}
		for _, g := range next {
			if oldModels[g] == nil {
				errs = append(errs, g.Disable())
			}
		}
		for _, g := range previous {
			errs = append(errs, g.updateModel(oldModels[g]))
			if a.enabled.Load() {
				errs = append(errs, g.Enable())
			}
		}
		errs = append(errs, order(previous))
		return nil, errors.Join(errs...)
	}
	for i, g := range next {
		if err := g.updateModel(modelFromSpec(specs[i])); err != nil {
			return rollback(err)
		}
		if a.enabled.Load() {
			if err := g.Enable(); err != nil {
				return rollback(err)
			}
		}
	}
	for _, g := range previous {
		if !seen[g.IDValue()] {
			if err := g.Disable(); err != nil {
				return rollback(err)
			}
		}
	}
	if err := order(next); err != nil {
		return rollback(err)
	}
	return next, nil
}
