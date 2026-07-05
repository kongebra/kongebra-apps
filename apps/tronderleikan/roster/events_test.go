package main

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestPersonEventSubjectsAndPayloads(t *testing.T) {
	tenant := uuid.New()
	p := Person{ID: uuid.New(), TenantID: tenant, Name: "Ada", Department: strptr("Eng")}

	t.Run("created", func(t *testing.T) {
		env, err := personCreatedEvent(p)
		if err != nil {
			t.Fatalf("personCreatedEvent: %v", err)
		}
		if env.Type != "tl.roster.person.created" {
			t.Errorf("type = %q", env.Type)
		}
		if env.TenantID != tenant {
			t.Errorf("tenant = %s, vil ha %s", env.TenantID, tenant)
		}
		var d personCreatedData
		if err := json.Unmarshal(env.Data, &d); err != nil {
			t.Fatalf("unmarshal data: %v", err)
		}
		if d.PersonID != p.ID || d.Name != "Ada" || d.Department == nil || *d.Department != "Eng" {
			t.Errorf("data = %+v", d)
		}
	})

	t.Run("account_claimed satt og fjernet", func(t *testing.T) {
		linked := p
		linked.AccountID = strptr("sub-1")
		env, err := personAccountEvent(linked)
		if err != nil {
			t.Fatalf("personAccountEvent: %v", err)
		}
		if env.Type != "tl.roster.person.account_claimed" {
			t.Errorf("type = %q", env.Type)
		}
		var d personAccountData
		_ = json.Unmarshal(env.Data, &d)
		if d.AccountID == nil || *d.AccountID != "sub-1" {
			t.Errorf("account_id = %v, vil ha sub-1", d.AccountID)
		}

		env, _ = personAccountEvent(p) // AccountID nil = unlink
		_ = json.Unmarshal(env.Data, &d)
		if d.AccountID != nil {
			t.Errorf("unlink: account_id = %v, vil ha nil", d.AccountID)
		}
	})

	t.Run("deleted", func(t *testing.T) {
		env, err := personDeletedEvent(tenant, p.ID)
		if err != nil {
			t.Fatalf("personDeletedEvent: %v", err)
		}
		if env.Type != "tl.roster.person.deleted" {
			t.Errorf("type = %q", env.Type)
		}
		var d personDeletedData
		_ = json.Unmarshal(env.Data, &d)
		if d.PersonID != p.ID {
			t.Errorf("person_id = %s, vil ha %s", d.PersonID, p.ID)
		}
	})
}
