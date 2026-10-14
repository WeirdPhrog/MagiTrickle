package magitrickle

import (
	"errors"
	"fmt"

	"magitrickle/app"
	groupruntime "magitrickle/groups"
	"magitrickle/models"
	"magitrickle/utils/intID"
)

func validateGroups(groups []*models.Group) error {
	ids := make(map[intID.ID]bool, len(groups))
	for _, group := range groups {
		if group == nil {
			return fmt.Errorf("null group")
		}
		if ids[group.ID] {
			return ErrGroupIDConflict
		}
		ids[group.ID] = true
		ruleIDs := make(map[intID.ID]bool, len(group.Rules))
		for _, rule := range group.Rules {
			if rule == nil {
				return fmt.Errorf("null rule in group %s", group.ID)
			}
			if ruleIDs[rule.ID] {
				return ErrRuleIDConflict
			}
			ruleIDs[rule.ID] = true
		}
	}
	return nil
}

func (a *App) UpdateGroup(model *models.Group) error {
	if err := validateGroups([]*models.Group{model}); err != nil {
		return err
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	for _, group := range a.userRuleSets {
		if group.IDValue() == model.ID {
			return group.updateModel(model)
		}
	}
	return app.ErrGroupNotFound
}

// ReplaceGroups validates the whole request before touching live routing and
// restores the previous groups if applying any replacement fails.
func (a *App) ReplaceGroups(models []*models.Group) error {
	if err := validateGroups(models); err != nil {
		return err
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.replaceGroupsLocked(models)
}

func (a *App) replaceGroupsLocked(models []*models.Group) error {
	next := make([]*RuleSet, 0, len(models))
	for _, model := range models {
		group, err := NewRuleSet(groupruntime.BuildRuntimeRuleSet(model), a)
		if err != nil {
			return err
		}
		next = append(next, group)
	}
	previous := a.userRuleSets
	enable := func(groups []*RuleSet) error {
		if !a.enabled.Load() {
			return nil
		}
		for _, group := range groups {
			if err := group.Enable(); err != nil {
				return err
			}
			if err := group.Sync(); err != nil {
				return err
			}
		}
		return nil
	}
	if err := disableRuleSets(previous); err != nil {
		return errors.Join(err, enable(previous))
	}
	if err := enable(next); err != nil {
		return errors.Join(err, disableRuleSets(next), enable(previous))
	}
	a.userRuleSets = next
	return nil
}
