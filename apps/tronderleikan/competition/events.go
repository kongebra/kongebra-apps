package main

import (
	"github.com/google/uuid"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/event"
)

// Competition-events (SPEC §9 event-katalog):
//
//	tl.competition.tournament.created
//	tl.competition.game.created | updated | finalized
//	tl.competition.participation.registered
//	tl.competition.result.recorded
//
// Alle skrives til outbox i SAMME tx som domene-endringen (events.go bygger bare
// envelopene; store-metodene skriver dem). Ingen lytter ennå (rating/live er
// fase 2-3), men events fyrer fra dag 1 (SPEC §7) - rating/live blir ren addisjon.
const (
	eventService = "competition"

	entityTournament    = "tournament"
	entityGame          = "game"
	entityParticipation = "participation"
	entityResult        = "result"

	eventCreated    = "created"
	eventUpdated    = "updated"
	eventFinalized  = "finalized"
	eventRegistered = "registered"
	eventRecorded   = "recorded"
)

// --- tournament ---

type tournamentCreatedData struct {
	TournamentID uuid.UUID `json:"tournament_id"`
	Name         string    `json:"name"`
	Year         *int      `json:"year,omitempty"`
}

func tournamentCreatedEvent(t Tournament) (event.Envelope, error) {
	return event.New(t.TenantID, event.Subject(eventService, entityTournament, eventCreated),
		tournamentCreatedData{TournamentID: t.ID, Name: t.Name, Year: t.Year})
}

// --- game ---

type gameData struct {
	GameID           uuid.UUID `json:"game_id"`
	TournamentID     uuid.UUID `json:"tournament_id"`
	Title            string    `json:"title"`
	Category         string    `json:"category"`
	RequiresApproval bool      `json:"requires_approval"`
	Status           string    `json:"status"`
}

func gameDataOf(g Game) gameData {
	return gameData{
		GameID:           g.ID,
		TournamentID:     g.TournamentID,
		Title:            g.Title,
		Category:         g.Category,
		RequiresApproval: g.RequiresApproval,
		Status:           g.Status,
	}
}

func gameCreatedEvent(g Game) (event.Envelope, error) {
	return event.New(g.TenantID, event.Subject(eventService, entityGame, eventCreated), gameDataOf(g))
}

func gameUpdatedEvent(g Game) (event.Envelope, error) {
	return event.New(g.TenantID, event.Subject(eventService, entityGame, eventUpdated), gameDataOf(g))
}

func gameFinalizedEvent(g Game) (event.Envelope, error) {
	return event.New(g.TenantID, event.Subject(eventService, entityGame, eventFinalized), gameDataOf(g))
}

// --- participation ---

type participationRegisteredData struct {
	GameID        uuid.UUID  `json:"game_id"`
	ParticipantID uuid.UUID  `json:"participant_id"`
	Type          string     `json:"type"`
	PersonID      *uuid.UUID `json:"person_id,omitempty"`
	TeamID        *uuid.UUID `json:"team_id,omitempty"`
}

func participationRegisteredEvent(p Participant) (event.Envelope, error) {
	return event.New(p.TenantID, event.Subject(eventService, entityParticipation, eventRegistered),
		participationRegisteredData{
			GameID:        p.GameID,
			ParticipantID: p.ID,
			Type:          p.Type,
			PersonID:      p.PersonID,
			TeamID:        p.TeamID,
		})
}

// --- result ---

type placementData struct {
	ParticipantID uuid.UUID `json:"participant_id"`
	Rank          int       `json:"rank"`
}

type resultRecordedData struct {
	GameID     uuid.UUID       `json:"game_id"`
	Placements []placementData `json:"placements"`
}

func resultRecordedEvent(tenantID, gameID uuid.UUID, results []PlacementResult) (event.Envelope, error) {
	placements := make([]placementData, 0, len(results))
	for _, r := range results {
		placements = append(placements, placementData{ParticipantID: r.ParticipantID, Rank: r.Rank})
	}
	return event.New(tenantID, event.Subject(eventService, entityResult, eventRecorded),
		resultRecordedData{GameID: gameID, Placements: placements})
}
