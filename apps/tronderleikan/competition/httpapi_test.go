package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/authn"
)

// --- fakes ---

// fakeStore implementerer Store. Kun metodene en test trenger settes; resten
// returnerer nullverdier (nok for auth/routing/feilmapping-testene).
type fakeStore struct {
	createTournamentFn    func(context.Context, uuid.UUID, TournamentInput) (Tournament, error)
	getTournamentFn       func(context.Context, uuid.UUID, uuid.UUID) (Tournament, error)
	createGameFn          func(context.Context, uuid.UUID, GameInput) (Game, error)
	finalizeGameFn        func(context.Context, uuid.UUID, uuid.UUID) (Game, error)
	createTeamFn          func(context.Context, uuid.UUID, TeamInput) (Team, error)
	registerParticipantFn func(context.Context, uuid.UUID, ParticipantInput) (Participant, error)
	recordResultsFn       func(context.Context, uuid.UUID, uuid.UUID, []PlacementInput) ([]PlacementResult, error)
}

func (f *fakeStore) CreateTournament(ctx context.Context, t uuid.UUID, in TournamentInput) (Tournament, error) {
	if f.createTournamentFn != nil {
		return f.createTournamentFn(ctx, t, in)
	}
	return Tournament{}, nil
}
func (f *fakeStore) ListTournaments(context.Context, uuid.UUID) ([]Tournament, error) {
	return nil, nil
}
func (f *fakeStore) GetTournament(ctx context.Context, t, id uuid.UUID) (Tournament, error) {
	if f.getTournamentFn != nil {
		return f.getTournamentFn(ctx, t, id)
	}
	return Tournament{}, nil
}
func (f *fakeStore) UpdateTournament(context.Context, uuid.UUID, uuid.UUID, TournamentInput) (Tournament, error) {
	return Tournament{}, nil
}
func (f *fakeStore) DeleteTournament(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (f *fakeStore) CreateGame(ctx context.Context, t uuid.UUID, in GameInput) (Game, error) {
	if f.createGameFn != nil {
		return f.createGameFn(ctx, t, in)
	}
	return Game{}, nil
}
func (f *fakeStore) ListGames(context.Context, uuid.UUID, *uuid.UUID) ([]Game, error) {
	return nil, nil
}
func (f *fakeStore) GetGame(context.Context, uuid.UUID, uuid.UUID) (Game, error) { return Game{}, nil }
func (f *fakeStore) UpdateGame(context.Context, uuid.UUID, uuid.UUID, GameInput) (Game, error) {
	return Game{}, nil
}
func (f *fakeStore) FinalizeGame(ctx context.Context, t, id uuid.UUID) (Game, error) {
	if f.finalizeGameFn != nil {
		return f.finalizeGameFn(ctx, t, id)
	}
	return Game{}, nil
}
func (f *fakeStore) DeleteGame(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (f *fakeStore) CreateTeam(ctx context.Context, t uuid.UUID, in TeamInput) (Team, error) {
	if f.createTeamFn != nil {
		return f.createTeamFn(ctx, t, in)
	}
	return Team{}, nil
}
func (f *fakeStore) ListTeams(context.Context, uuid.UUID, uuid.UUID) ([]Team, error) {
	return nil, nil
}
func (f *fakeStore) GetTeam(context.Context, uuid.UUID, uuid.UUID) (Team, error) { return Team{}, nil }
func (f *fakeStore) DeleteTeam(context.Context, uuid.UUID, uuid.UUID) error      { return nil }

func (f *fakeStore) RegisterParticipant(ctx context.Context, t uuid.UUID, in ParticipantInput) (Participant, error) {
	if f.registerParticipantFn != nil {
		return f.registerParticipantFn(ctx, t, in)
	}
	return Participant{}, nil
}
func (f *fakeStore) ListParticipants(context.Context, uuid.UUID, uuid.UUID) ([]Participant, error) {
	return nil, nil
}
func (f *fakeStore) GetParticipant(context.Context, uuid.UUID, uuid.UUID) (Participant, error) {
	return Participant{}, nil
}
func (f *fakeStore) DeleteParticipant(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (f *fakeStore) RecordResults(ctx context.Context, t, g uuid.UUID, p []PlacementInput) ([]PlacementResult, error) {
	if f.recordResultsFn != nil {
		return f.recordResultsFn(ctx, t, g, p)
	}
	return nil, nil
}
func (f *fakeStore) ListResults(context.Context, uuid.UUID, uuid.UUID) ([]PlacementResult, error) {
	return nil, nil
}

// fakeVisibility svarer med et fast public-flagg.
type fakeVisibility struct{ public bool }

func (f fakeVisibility) IsPublic(context.Context, uuid.UUID) (bool, error) { return f.public, nil }

// fakeValidator mapper token-strenger til principals (ingen ekte JWT).
type fakeValidator struct{}

func (fakeValidator) Validate(_ context.Context, raw string) (authn.Principal, error) {
	switch raw {
	case "organizer":
		return authn.Principal{UserID: "u-org", Roles: []string{authn.RoleOrganizer}}, nil
	case "player":
		return authn.Principal{UserID: "u-play", Roles: []string{authn.RolePlayer}}, nil
	default:
		return authn.Principal{}, errors.New("invalid token")
	}
}

// fakeRoster styrer ref-validering per test.
type fakeRoster struct {
	exists bool
	err    error
}

func (f fakeRoster) PersonExists(context.Context, uuid.UUID, uuid.UUID, string) (bool, error) {
	return f.exists, f.err
}

func newTestAPI(store Store, public bool, roster RefValidator) http.Handler {
	if roster == nil {
		roster = fakeRoster{exists: true}
	}
	a := &api{store: store, vis: fakeVisibility{public: public}, validator: fakeValidator{}, roster: roster}
	return a.routes()
}

func req(method, path, token, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

var testTenant = uuid.MustParse("11111111-1111-7111-8111-111111111111")

const base = "/api/competition/tenants/11111111-1111-7111-8111-111111111111"

// --- write-autorisasjon (SPEC §6: organizer kreves for skriv) ---

func TestWriteRequiresOrganizer(t *testing.T) {
	store := &fakeStore{createTournamentFn: func(context.Context, uuid.UUID, TournamentInput) (Tournament, error) {
		return Tournament{ID: uuid.New(), TenantID: testTenant, Name: "TL"}, nil
	}}
	h := newTestAPI(store, true, nil)

	cases := map[string]struct {
		token string
		want  int
	}{
		"uten token -> 401":    {"", http.StatusUnauthorized},
		"ugyldig token -> 401": {"tull", http.StatusUnauthorized},
		"player -> 403":        {"player", http.StatusForbidden},
		"organizer -> 201":     {"organizer", http.StatusCreated},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req(http.MethodPost, base+"/tournaments", tc.token, `{"name":"TL"}`))
			if rec.Code != tc.want {
				t.Errorf("status = %d, vil ha %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// --- read-gate (SPEC §6: anonym lese hvis public_visibility på) ---

func TestReadGate(t *testing.T) {
	store := &fakeStore{}
	path := base + "/tournaments"

	t.Run("anonym + offentlig -> 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newTestAPI(store, true, nil).ServeHTTP(rec, req(http.MethodGet, path, "", ""))
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d", rec.Code)
		}
	})
	t.Run("anonym + ikke-offentlig -> 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newTestAPI(store, false, nil).ServeHTTP(rec, req(http.MethodGet, path, "", ""))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d", rec.Code)
		}
	})
	t.Run("autentisert + ikke-offentlig -> 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newTestAPI(store, false, nil).ServeHTTP(rec, req(http.MethodGet, path, "player", ""))
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d", rec.Code)
		}
	})
}

// --- ref-validering (SPEC §7: person_id valideres mot roster FØR persist) ---

func TestParticipantRefValidation(t *testing.T) {
	person := uuid.New()
	game := uuid.New()
	path := base + "/games/" + game.String() + "/participants"
	body := `{"type":"person","person_id":"` + person.String() + `"}`

	var persisted bool
	store := &fakeStore{registerParticipantFn: func(_ context.Context, _ uuid.UUID, in ParticipantInput) (Participant, error) {
		persisted = true
		return Participant{ID: uuid.New(), GameID: in.GameID, Type: in.Type, PersonID: in.PersonID}, nil
	}}

	t.Run("person finnes i roster -> 201 og game_id fra path", func(t *testing.T) {
		persisted = false
		var gotGame uuid.UUID
		s := &fakeStore{registerParticipantFn: func(_ context.Context, _ uuid.UUID, in ParticipantInput) (Participant, error) {
			gotGame = in.GameID
			return Participant{ID: uuid.New(), GameID: in.GameID}, nil
		}}
		rec := httptest.NewRecorder()
		newTestAPI(s, true, fakeRoster{exists: true}).ServeHTTP(rec, req(http.MethodPost, path, "organizer", body))
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
		}
		if gotGame != game {
			t.Errorf("game_id sendt til store = %s, vil ha %s (fra path)", gotGame, game)
		}
	})

	t.Run("person finnes ikke i roster -> 422, ingen persist", func(t *testing.T) {
		persisted = false
		rec := httptest.NewRecorder()
		newTestAPI(store, true, fakeRoster{exists: false}).ServeHTTP(rec, req(http.MethodPost, path, "organizer", body))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, vil ha 422 (body %s)", rec.Code, rec.Body.String())
		}
		if persisted {
			t.Error("persist skjedde selv om ref-validering feilet")
		}
	})

	t.Run("roster nede -> 502, ingen persist", func(t *testing.T) {
		persisted = false
		rec := httptest.NewRecorder()
		newTestAPI(store, true, fakeRoster{err: ErrRosterUnavailable}).ServeHTTP(rec, req(http.MethodPost, path, "organizer", body))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, vil ha 502 (body %s)", rec.Code, rec.Body.String())
		}
		if persisted {
			t.Error("persist skjedde selv om roster var nede")
		}
	})
}

func TestTeamRefValidation(t *testing.T) {
	game := uuid.New()
	m1, m2 := uuid.New(), uuid.New()
	path := base + "/games/" + game.String() + "/teams"
	body := `{"name":"Laget","members":["` + m1.String() + `","` + m2.String() + `"]}`

	t.Run("alle medlemmer finnes -> 201", func(t *testing.T) {
		s := &fakeStore{createTeamFn: func(_ context.Context, _ uuid.UUID, in TeamInput) (Team, error) {
			return Team{ID: uuid.New(), GameID: in.GameID, Name: in.Name, Members: in.Members}, nil
		}}
		rec := httptest.NewRecorder()
		newTestAPI(s, true, fakeRoster{exists: true}).ServeHTTP(rec, req(http.MethodPost, path, "organizer", body))
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
		}
	})
	t.Run("et medlem mangler i roster -> 422", func(t *testing.T) {
		var persisted bool
		s := &fakeStore{createTeamFn: func(context.Context, uuid.UUID, TeamInput) (Team, error) {
			persisted = true
			return Team{}, nil
		}}
		rec := httptest.NewRecorder()
		newTestAPI(s, true, fakeRoster{exists: false}).ServeHTTP(rec, req(http.MethodPost, path, "organizer", body))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, vil ha 422", rec.Code)
		}
		if persisted {
			t.Error("persist skjedde selv om et medlem manglet")
		}
	})
}

// --- feilmapping ---

func TestErrorMapping(t *testing.T) {
	game := uuid.New()

	t.Run("not found -> 404", func(t *testing.T) {
		s := &fakeStore{getTournamentFn: func(context.Context, uuid.UUID, uuid.UUID) (Tournament, error) {
			return Tournament{}, ErrNotFound
		}}
		rec := httptest.NewRecorder()
		newTestAPI(s, true, nil).ServeHTTP(rec, req(http.MethodGet, base+"/tournaments/"+uuid.New().String(), "", ""))
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d", rec.Code)
		}
	})
	t.Run("ref not found -> 422", func(t *testing.T) {
		s := &fakeStore{createGameFn: func(context.Context, uuid.UUID, GameInput) (Game, error) {
			return Game{}, ErrRefNotFound
		}}
		rec := httptest.NewRecorder()
		body := `{"tournament_id":"` + uuid.New().String() + `","title":"Quiz","category":"quiz"}`
		newTestAPI(s, true, nil).ServeHTTP(rec, req(http.MethodPost, base+"/games", "organizer", body))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, vil ha 422", rec.Code)
		}
	})
	t.Run("game finalized -> 409", func(t *testing.T) {
		s := &fakeStore{recordResultsFn: func(context.Context, uuid.UUID, uuid.UUID, []PlacementInput) ([]PlacementResult, error) {
			return nil, ErrGameFinalized
		}}
		rec := httptest.NewRecorder()
		body := `{"placements":[{"participant_id":"` + uuid.New().String() + `","rank":1}]}`
		newTestAPI(s, true, nil).ServeHTTP(rec, req(http.MethodPost, base+"/games/"+game.String()+"/results", "organizer", body))
		if rec.Code != http.StatusConflict {
			t.Errorf("status = %d, vil ha 409", rec.Code)
		}
	})
	t.Run("ugyldig json -> 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newTestAPI(&fakeStore{}, true, nil).ServeHTTP(rec, req(http.MethodPost, base+"/tournaments", "organizer", `{"unknown":1}`))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, vil ha 400", rec.Code)
		}
	})
	t.Run("ugyldig id i path -> 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newTestAPI(&fakeStore{}, true, nil).ServeHTTP(rec, req(http.MethodGet, base+"/tournaments/not-a-uuid", "", ""))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, vil ha 400", rec.Code)
		}
	})
}
