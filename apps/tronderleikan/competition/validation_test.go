package main

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func strptr(s string) *string { return &s }
func intptr(i int) *int       { return &i }

func TestTournamentInputNormalize(t *testing.T) {
	t.Run("trimmer navn, tom description -> nil", func(t *testing.T) {
		out, err := TournamentInput{Name: "  TL 2026  ", Description: strptr("  ")}.normalize()
		if err != nil {
			t.Fatalf("normalize: %v", err)
		}
		if out.Name != "TL 2026" || out.Description != nil {
			t.Fatalf("out = %+v", out)
		}
	})
	t.Run("tomt navn -> ErrInvalidInput", func(t *testing.T) {
		if _, err := (TournamentInput{Name: "  "}).normalize(); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("year utenfor område -> ErrInvalidInput", func(t *testing.T) {
		if _, err := (TournamentInput{Name: "x", Year: intptr(1800)}).normalize(); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestGameInputNormalize(t *testing.T) {
	tour := uuid.New()
	t.Run("krever tournament_id ved opprett", func(t *testing.T) {
		if _, err := (GameInput{Title: "Quiz", Category: "quiz"}).normalize(true); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("krever category", func(t *testing.T) {
		if _, err := (GameInput{TournamentID: tour, Title: "Quiz"}).normalize(true); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("gyldig", func(t *testing.T) {
		out, err := (GameInput{TournamentID: tour, Title: " Quiz ", Category: " quiz ", RequiresApproval: true}).normalize(true)
		if err != nil || out.Title != "Quiz" || out.Category != "quiz" || !out.RequiresApproval {
			t.Fatalf("out = %+v err = %v", out, err)
		}
	})
	t.Run("oppdater krever ikke tournament_id", func(t *testing.T) {
		if _, err := (GameInput{Title: "Quiz", Category: "quiz"}).normalize(false); err != nil {
			t.Errorf("err = %v", err)
		}
	})
}

func TestParticipantInputNormalize(t *testing.T) {
	game := uuid.New()
	person := uuid.New()
	team := uuid.New()

	t.Run("person: person_id kreves", func(t *testing.T) {
		if _, err := (ParticipantInput{GameID: game, Type: ParticipantPerson}).normalize(); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("person: team_id forbudt", func(t *testing.T) {
		in := ParticipantInput{GameID: game, Type: ParticipantPerson, PersonID: &person, TeamID: &team}
		if _, err := in.normalize(); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("person: gyldig renser team_id", func(t *testing.T) {
		out, err := (ParticipantInput{GameID: game, Type: ParticipantPerson, PersonID: &person}).normalize()
		if err != nil || out.PersonID == nil || *out.PersonID != person || out.TeamID != nil {
			t.Fatalf("out = %+v err = %v", out, err)
		}
	})
	t.Run("team: team_id kreves", func(t *testing.T) {
		if _, err := (ParticipantInput{GameID: game, Type: ParticipantTeam}).normalize(); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("team: gyldig", func(t *testing.T) {
		out, err := (ParticipantInput{GameID: game, Type: ParticipantTeam, TeamID: &team}).normalize()
		if err != nil || out.TeamID == nil || *out.TeamID != team || out.PersonID != nil {
			t.Fatalf("out = %+v err = %v", out, err)
		}
	})
	t.Run("ukjent type", func(t *testing.T) {
		if _, err := (ParticipantInput{GameID: game, Type: "robot", PersonID: &person}).normalize(); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestTeamInputNormalize(t *testing.T) {
	game := uuid.New()
	a, b := uuid.New(), uuid.New()

	t.Run("dedupliserer medlemmer", func(t *testing.T) {
		out, err := (TeamInput{GameID: game, Name: "Laget", Members: []uuid.UUID{a, b, a}}).normalize()
		if err != nil {
			t.Fatalf("normalize: %v", err)
		}
		if len(out.Members) != 2 {
			t.Fatalf("members = %v, vil ha 2 unike", out.Members)
		}
	})
	t.Run("nil-medlem avvises", func(t *testing.T) {
		if _, err := (TeamInput{GameID: game, Name: "x", Members: []uuid.UUID{uuid.Nil}}).normalize(); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("krever game_id", func(t *testing.T) {
		if _, err := (TeamInput{Name: "x"}).normalize(); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestNormalizePlacements(t *testing.T) {
	p1, p2, p3 := uuid.New(), uuid.New(), uuid.New()

	t.Run("tom liste avvises", func(t *testing.T) {
		if _, err := normalizePlacements(nil); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("rank < 1 avvises", func(t *testing.T) {
		if _, err := normalizePlacements([]PlacementInput{{ParticipantID: p1, Rank: 0}}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("duplikat deltaker avvises", func(t *testing.T) {
		in := []PlacementInput{{ParticipantID: p1, Rank: 1}, {ParticipantID: p1, Rank: 2}}
		if _, err := normalizePlacements(in); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("ties er lov (samme rank, ulike deltakere)", func(t *testing.T) {
		in := []PlacementInput{{ParticipantID: p1, Rank: 1}, {ParticipantID: p2, Rank: 1}, {ParticipantID: p3, Rank: 3}}
		out, err := normalizePlacements(in)
		if err != nil {
			t.Fatalf("ties skulle være lov: %v", err)
		}
		if len(out) != 3 {
			t.Fatalf("out = %v", out)
		}
	})
}
