package main

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestEventSubjectsMatchCatalog(t *testing.T) {
	tenant := uuid.New()
	tour := Tournament{ID: uuid.New(), TenantID: tenant, Name: "TL"}
	game := Game{ID: uuid.New(), TenantID: tenant, TournamentID: tour.ID, Title: "Quiz", Category: "quiz", Status: GameStatusOpen}
	person := uuid.New()
	part := Participant{ID: uuid.New(), TenantID: tenant, GameID: game.ID, Type: ParticipantPerson, PersonID: &person}

	cases := []struct {
		name    string
		build   func() (envType string, err error)
		subject string
	}{
		{"tournament.created", func() (string, error) {
			e, err := tournamentCreatedEvent(tour)
			return e.Type, err
		}, "tl.competition.tournament.created"},
		{"game.created", func() (string, error) { e, err := gameCreatedEvent(game); return e.Type, err }, "tl.competition.game.created"},
		{"game.updated", func() (string, error) { e, err := gameUpdatedEvent(game); return e.Type, err }, "tl.competition.game.updated"},
		{"game.finalized", func() (string, error) { e, err := gameFinalizedEvent(game); return e.Type, err }, "tl.competition.game.finalized"},
		{"participation.registered", func() (string, error) {
			e, err := participationRegisteredEvent(part)
			return e.Type, err
		}, "tl.competition.participation.registered"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.build()
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if got != tc.subject {
				t.Errorf("subject = %q, vil ha %q", got, tc.subject)
			}
		})
	}
}

func TestResultRecordedEventData(t *testing.T) {
	tenant := uuid.New()
	game := uuid.New()
	results := []PlacementResult{
		{ParticipantID: uuid.New(), Rank: 1},
		{ParticipantID: uuid.New(), Rank: 1}, // tie
		{ParticipantID: uuid.New(), Rank: 3},
	}
	env, err := resultRecordedEvent(tenant, game, results)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if env.Type != "tl.competition.result.recorded" || env.TenantID != tenant {
		t.Fatalf("env = %+v", env)
	}
	var data resultRecordedData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data.GameID != game || len(data.Placements) != 3 {
		t.Fatalf("data = %+v", data)
	}
	// Ties bevart i eventet: to plasseringer med rank 1.
	ties := 0
	for _, p := range data.Placements {
		if p.Rank == 1 {
			ties++
		}
	}
	if ties != 2 {
		t.Errorf("rank-1 plasseringer = %d, vil ha 2 (ties)", ties)
	}
}
