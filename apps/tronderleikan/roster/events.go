package main

import (
	"github.com/google/uuid"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/event"
)

// Roster-events (SPEC §9 event-katalog):
//
//	tl.roster.person.created | updated | deleted | account_claimed
//
// Alle skrives til outbox i SAMME tx som domene-endringen (events.go bygger
// bare envelopene; store.go skriver dem). account_claimed dekker den manuelle
// account-koblingen i v1 (SPEC §4) - samme event brukes av claim-flyten i v2.
const (
	eventService     = "roster"
	entityPerson     = "person"
	eventCreated     = "created"
	eventUpdated     = "updated"
	eventDeleted     = "deleted"
	eventAccountLink = "account_claimed"
)

// personCreatedData er payloaden for tl.roster.person.created. Konsumenter
// (competition m.fl.) trenger nok til å holde sin egen person-referanse à jour.
type personCreatedData struct {
	PersonID   uuid.UUID `json:"person_id"`
	Name       string    `json:"name"`
	Department *string   `json:"department,omitempty"`
}

type personUpdatedData struct {
	PersonID   uuid.UUID `json:"person_id"`
	Name       string    `json:"name"`
	Department *string   `json:"department,omitempty"`
}

type personDeletedData struct {
	PersonID uuid.UUID `json:"person_id"`
}

// personAccountData er payloaden for account_claimed. AccountID er nil når
// koblingen fjernes (unlink), satt når den etableres/endres.
type personAccountData struct {
	PersonID  uuid.UUID `json:"person_id"`
	AccountID *string   `json:"account_id"`
}

func personCreatedEvent(p Person) (event.Envelope, error) {
	return event.New(p.TenantID, event.Subject(eventService, entityPerson, eventCreated),
		personCreatedData{PersonID: p.ID, Name: p.Name, Department: p.Department})
}

func personUpdatedEvent(p Person) (event.Envelope, error) {
	return event.New(p.TenantID, event.Subject(eventService, entityPerson, eventUpdated),
		personUpdatedData{PersonID: p.ID, Name: p.Name, Department: p.Department})
}

func personDeletedEvent(tenantID, personID uuid.UUID) (event.Envelope, error) {
	return event.New(tenantID, event.Subject(eventService, entityPerson, eventDeleted),
		personDeletedData{PersonID: personID})
}

func personAccountEvent(p Person) (event.Envelope, error) {
	return event.New(p.TenantID, event.Subject(eventService, entityPerson, eventAccountLink),
		personAccountData{PersonID: p.ID, AccountID: p.AccountID})
}
