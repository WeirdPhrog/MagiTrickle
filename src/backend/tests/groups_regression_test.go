package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"magitrickle"
	"magitrickle/api/v1"
	"magitrickle/api/v1/types"
	"magitrickle/models"
	"magitrickle/utils/intID"
)

func TestGroupEditsReplaceCompiledRulesAndKeepSnapshots(t *testing.T) {
	a := &magitrickle.App{}
	id, ruleID := intID.ID{1}, intID.ID{2}
	old := &models.Rule{ID: ruleID, Type: models.RuleTypeRegEx, Rule: "^old\\.example$", Enable: true}
	group := &models.Group{ID: id, Name: "old", Enable: true, Rules: []*models.Rule{old}}
	if err := a.AddGroup(group); err != nil {
		t.Fatal(err)
	}
	if !old.IsMatch("old.example") {
		t.Fatal("initial regex does not match")
	}
	router := v1.NewRouter(a)
	request := func(path string, payload any) *httptest.ResponseRecorder {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, path, bytes.NewReader(data)))
		return w
	}
	w := request("/groups/"+id.String()+"/rules/"+ruleID.String(), types.RuleReq{Type: models.RuleTypeRegEx, Rule: "^new\\.example$", Enable: true})
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	updated := a.UserGroups()[0].Model().Rules[0]
	if !updated.IsMatch("new.example") || updated.IsMatch("old.example") {
		t.Fatal("edited regex still uses old compiled matcher")
	}
	if group.Rules[0] != old || !old.IsMatch("old.example") {
		t.Fatal("published model mutated")
	}
	wrong := intID.ID{9}
	w = request("/groups/"+id.String(), types.GroupReq{ID: &wrong, Name: "bad"})
	if w.Code != http.StatusBadRequest || a.UserGroups()[0].Model().Name != "old" {
		t.Fatal("invalid edit changed existing group")
	}
	w = request("/groups", types.GroupsReq{Groups: &[]types.GroupReq{{ID: &id, Name: "first"}, {ID: &id, Name: "duplicate"}}})
	if w.Code != http.StatusBadRequest || len(a.UserGroups()) != 1 || a.UserGroups()[0].Model().Name != "old" {
		t.Fatal("invalid bulk edit replaced groups")
	}
}

func TestConcurrentGroupAPIAndDNSMatching(t *testing.T) {
	a := &magitrickle.App{}
	id, ruleID := intID.ID{1}, intID.ID{2}
	if err := a.AddGroup(&models.Group{ID: id, Rules: []*models.Rule{{ID: ruleID, Type: models.RuleTypeDomain, Rule: "example.com", Enable: true}}}); err != nil {
		t.Fatal(err)
	}
	router := v1.NewRouter(a)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 50; n++ {
				data := []byte(`{"type":"regex","rule":"^example\\.com$","enable":true}`)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/groups/"+id.String()+"/rules/"+ruleID.String(), bytes.NewReader(data)))
				if w.Code != http.StatusOK {
					t.Errorf("edit: %d %s", w.Code, w.Body.String())
					return
				}
				for _, group := range a.UserGroups() {
					for _, rule := range group.Model().Rules {
						rule.IsMatch("example.com")
					}
				}
			}
		}()
	}
	wg.Wait()
}
