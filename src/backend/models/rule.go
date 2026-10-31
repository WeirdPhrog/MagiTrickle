package models

import (
	"fmt"
	"strings"

	"magitrickle/utils/intID"
)

const (
	RuleTypeDomain    string = "domain"
	RuleTypeNamespace string = "namespace"
	RuleTypeWildcard  string = "wildcard"
	RuleTypeSubnet    string = "subnet"
	RuleTypeSubnet6   string = "subnet6"
)

type Rule struct {
	ID     intID.ID `yaml:"id"`
	Name   string   `yaml:"name"`
	Type   string   `yaml:"type"`
	Rule   string   `yaml:"rule"`
	Enable bool     `yaml:"enable"`
}

func (d *Rule) IsEnabled() bool {
	return d.Enable
}

func ValidateRuleType(ruleType string) error {
	switch ruleType {
	case RuleTypeDomain, RuleTypeNamespace, RuleTypeWildcard, RuleTypeSubnet, RuleTypeSubnet6:
		return nil
	default:
		return fmt.Errorf("unsupported rule type %q", ruleType)
	}
}

func (d *Rule) IsMatch(domainName string) bool {
	switch d.Type {
	case RuleTypeDomain:
		return strings.EqualFold(domainName, d.Rule)

	case RuleTypeNamespace:
		if strings.EqualFold(domainName, d.Rule) {
			return true
		}

		ruleLen := len(d.Rule)
		domainLen := len(domainName)
		if domainLen < ruleLen+1 {
			return false
		}
		return domainName[domainLen-ruleLen-1] == '.' && strings.EqualFold(domainName[domainLen-ruleLen:], d.Rule)

	case RuleTypeWildcard:
		return matchWildcard(strings.ToLower(d.Rule), strings.ToLower(domainName))
	}
	return false
}

// matchWildcard compares a whole name with a pattern: '*' is any sequence of
// characters, including an empty one, '?' is exactly one character, and
// everything else, including '.', is literal.
func matchWildcard(pattern, name string) bool {
	p, n := 0, 0
	star, retry := -1, 0
	for n < len(name) {
		switch {
		case p < len(pattern) && pattern[p] == '*':
			star, retry = p, n
			p++
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == name[n]):
			p++
			n++
		case star >= 0:
			// Go back to the last '*' and give it one more character.
			retry++
			p, n = star+1, retry
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
