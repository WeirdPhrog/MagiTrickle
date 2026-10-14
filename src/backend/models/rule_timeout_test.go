package models

import (
	"strings"
	"testing"
	"time"
)

func TestRegexBacktrackingHasDeadline(t *testing.T) {
	rule := &Rule{Type: RuleTypeRegEx, Rule: `^(a+)+$`}
	started := time.Now()
	if rule.IsMatch(strings.Repeat("a", 200) + "!") {
		t.Fatal("unexpected match")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("regex took %s", elapsed)
	}
	if !rule.IsMatch("aaaa") {
		t.Fatal("timeout poisoned subsequent matches")
	}
}

func TestRegexLookaheadRemainsSupported(t *testing.T) {
	rule := &Rule{Type: RuleTypeRegEx, Rule: `^(?=.*example).*\.com$`}
	if !rule.IsMatch("www.example.com") || rule.IsMatch("www.other.com") {
		t.Fatal("lookahead matching changed")
	}
}
