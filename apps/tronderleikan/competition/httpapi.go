package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/authn"
	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/tenantctx"
)

// tokenValidator er delmengden av *authn.Validator API-et bruker (fakes i test).
type tokenValidator interface {
	Validate(ctx context.Context, rawToken string) (authn.Principal, error)
}

// api holder avhengighetene HTTP-laget trenger.
type api struct {
	store     Store
	vis       Visibility
	validator tokenValidator
	roster    RefValidator
}

// routes bygger muxen (SPEC §10: /api/competition/..., tenant-UUID i path).
// Skriv krever organizer-rolle (pkg/authn). Les er gatet av readGate:
// autentisert tenant-tilgang ELLER anonym hvis tenant har public_visibility på.
func (a *api) routes() http.Handler {
	mux := http.NewServeMux()

	write := func(h http.HandlerFunc) http.Handler {
		return tenantctx.Middleware(a.authMiddleware(authn.RequireRole(authn.RoleOrganizer)(h)))
	}
	read := func(h http.HandlerFunc) http.Handler {
		return tenantctx.Middleware(a.readGate(h))
	}

	const base = "/api/competition/tenants/{tenant_id}"

	// Tournaments
	mux.Handle("POST "+base+"/tournaments", write(a.handleCreateTournament))
	mux.Handle("GET "+base+"/tournaments", read(a.handleListTournaments))
	mux.Handle("GET "+base+"/tournaments/{id}", read(a.handleGetTournament))
	mux.Handle("PUT "+base+"/tournaments/{id}", write(a.handleUpdateTournament))
	mux.Handle("DELETE "+base+"/tournaments/{id}", write(a.handleDeleteTournament))

	// Games
	mux.Handle("POST "+base+"/games", write(a.handleCreateGame))
	mux.Handle("GET "+base+"/games", read(a.handleListGames))
	mux.Handle("GET "+base+"/games/{id}", read(a.handleGetGame))
	mux.Handle("PUT "+base+"/games/{id}", write(a.handleUpdateGame))
	mux.Handle("POST "+base+"/games/{id}/finalize", write(a.handleFinalizeGame))
	mux.Handle("DELETE "+base+"/games/{id}", write(a.handleDeleteGame))

	// Teams (nested under game)
	mux.Handle("POST "+base+"/games/{game_id}/teams", write(a.handleCreateTeam))
	mux.Handle("GET "+base+"/games/{game_id}/teams", read(a.handleListTeams))
	mux.Handle("GET "+base+"/games/{game_id}/teams/{id}", read(a.handleGetTeam))
	mux.Handle("DELETE "+base+"/games/{game_id}/teams/{id}", write(a.handleDeleteTeam))

	// Participants (nested under game)
	mux.Handle("POST "+base+"/games/{game_id}/participants", write(a.handleRegisterParticipant))
	mux.Handle("GET "+base+"/games/{game_id}/participants", read(a.handleListParticipants))
	mux.Handle("GET "+base+"/games/{game_id}/participants/{id}", read(a.handleGetParticipant))
	mux.Handle("DELETE "+base+"/games/{game_id}/participants/{id}", write(a.handleDeleteParticipant))

	// Placement results (nested under game)
	mux.Handle("POST "+base+"/games/{game_id}/results", write(a.handleRecordResults))
	mux.Handle("GET "+base+"/games/{game_id}/results", read(a.handleListResults))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})
	return mux
}

// authMiddleware validerer Bearer-tokenet og legger principalen i context, slik
// pkg/authn.RequireRole kan håndheve rollen etterpå.
func (a *api) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.authenticate(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing or invalid token")
			return
		}
		next.ServeHTTP(w, r.WithContext(authn.WithPrincipal(r.Context(), p)))
	})
}

// readGate slipper gjennom autentiserte kall, ellers anonym lesing hvis tenanten
// har public_visibility på (SPEC §6). Ugyldig token = 401.
func (a *api) readGate(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if raw, ok := bearerToken(r); ok {
			p, err := a.validator.Validate(r.Context(), raw)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}
			next.ServeHTTP(w, r.WithContext(authn.WithPrincipal(r.Context(), p)))
			return
		}
		tenantID, ok := tenantctx.From(r.Context())
		if !ok {
			writeError(w, http.StatusBadRequest, "missing tenant")
			return
		}
		public, err := a.vis.IsPublic(r.Context(), tenantID)
		if err != nil {
			log.Printf("readGate: visibility-oppslag feilet: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !public {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *api) authenticate(r *http.Request) (authn.Principal, bool) {
	raw, ok := bearerToken(r)
	if !ok {
		return authn.Principal{}, false
	}
	p, err := a.validator.Validate(r.Context(), raw)
	if err != nil {
		return authn.Principal{}, false
	}
	return p, true
}

func bearerToken(r *http.Request) (string, bool) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || raw == "" {
		return "", false
	}
	return raw, true
}

// --- tournament handlers ---

func (a *api) handleCreateTournament(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	var in TournamentInput
	if !decodeJSON(w, r, &in) {
		return
	}
	t, err := a.store.CreateTournament(r.Context(), tenantID, in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (a *api) handleListTournaments(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	ts, err := a.store.ListTournaments(r.Context(), tenantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(ts))
}

func (a *api) handleGetTournament(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	t, err := a.store.GetTournament(r.Context(), tenantID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (a *api) handleUpdateTournament(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var in TournamentInput
	if !decodeJSON(w, r, &in) {
		return
	}
	t, err := a.store.UpdateTournament(r.Context(), tenantID, id, in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (a *api) handleDeleteTournament(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := a.store.DeleteTournament(r.Context(), tenantID, id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- game handlers ---

func (a *api) handleCreateGame(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	var in GameInput
	if !decodeJSON(w, r, &in) {
		return
	}
	g, err := a.store.CreateGame(r.Context(), tenantID, in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (a *api) handleListGames(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	var tournamentID *uuid.UUID
	if raw := r.URL.Query().Get("tournament_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid tournament_id")
			return
		}
		tournamentID = &id
	}
	gs, err := a.store.ListGames(r.Context(), tenantID, tournamentID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(gs))
}

func (a *api) handleGetGame(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	g, err := a.store.GetGame(r.Context(), tenantID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (a *api) handleUpdateGame(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var in GameInput
	if !decodeJSON(w, r, &in) {
		return
	}
	g, err := a.store.UpdateGame(r.Context(), tenantID, id, in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (a *api) handleFinalizeGame(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	g, err := a.store.FinalizeGame(r.Context(), tenantID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (a *api) handleDeleteGame(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := a.store.DeleteGame(r.Context(), tenantID, id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- team handlers ---

func (a *api) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	gameID, ok := pathUUID(w, r, "game_id")
	if !ok {
		return
	}
	var in TeamInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.GameID = gameID // path er sannheten
	// Ref-validering: alle medlemmer må finnes i roster FØR persist (SPEC §7).
	if !a.validatePersons(w, r, tenantID, in.Members) {
		return
	}
	t, err := a.store.CreateTeam(r.Context(), tenantID, in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (a *api) handleListTeams(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	gameID, ok := pathUUID(w, r, "game_id")
	if !ok {
		return
	}
	ts, err := a.store.ListTeams(r.Context(), tenantID, gameID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(ts))
}

func (a *api) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	t, err := a.store.GetTeam(r.Context(), tenantID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (a *api) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := a.store.DeleteTeam(r.Context(), tenantID, id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- participant handlers ---

func (a *api) handleRegisterParticipant(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	gameID, ok := pathUUID(w, r, "game_id")
	if !ok {
		return
	}
	var in ParticipantInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.GameID = gameID // path er sannheten
	// Ref-validering for person-deltakere: person_id må finnes i roster (SPEC §7).
	// Lag-deltakere håndheves av intra-service FK i store-laget.
	if in.Type == ParticipantPerson && in.PersonID != nil {
		if !a.validatePersons(w, r, tenantID, []uuid.UUID{*in.PersonID}) {
			return
		}
	}
	p, err := a.store.RegisterParticipant(r.Context(), tenantID, in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (a *api) handleListParticipants(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	gameID, ok := pathUUID(w, r, "game_id")
	if !ok {
		return
	}
	ps, err := a.store.ListParticipants(r.Context(), tenantID, gameID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(ps))
}

func (a *api) handleGetParticipant(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	p, err := a.store.GetParticipant(r.Context(), tenantID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *api) handleDeleteParticipant(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := a.store.DeleteParticipant(r.Context(), tenantID, id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- result handlers ---

// recordResultsRequest er payloaden for å registrere en plasseringsliste.
type recordResultsRequest struct {
	Placements []PlacementInput `json:"placements"`
}

func (a *api) handleRecordResults(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	gameID, ok := pathUUID(w, r, "game_id")
	if !ok {
		return
	}
	var req recordResultsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	results, err := a.store.RecordResults(r.Context(), tenantID, gameID, req.Placements)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(results))
}

func (a *api) handleListResults(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	gameID, ok := pathUUID(w, r, "game_id")
	if !ok {
		return
	}
	rs, err := a.store.ListResults(r.Context(), tenantID, gameID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(rs))
}

// --- ref-validering ---

// validatePersons sjekker at alle person-ID-ene finnes i roster (SPEC §7).
// Videresender kallerens organizer-token. Skriver HTTP-feil og returnerer false
// ved manglende ref (422) eller roster nede (502). true = alle finnes.
func (a *api) validatePersons(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID, personIDs []uuid.UUID) bool {
	token, _ := bearerToken(r) // write-ruter har alltid et gyldig token her
	for _, pid := range personIDs {
		exists, err := a.roster.PersonExists(r.Context(), tenantID, pid, token)
		if err != nil {
			log.Printf("ref-validering mot roster feilet: %v", err)
			writeError(w, http.StatusBadGateway, "could not validate person reference against roster")
			return false
		}
		if !exists {
			writeError(w, http.StatusUnprocessableEntity, "person "+pid.String()+" finnes ikke i roster")
			return false
		}
	}
	return true
}

// --- helpers ---

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return uuid.Nil, false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body: "+err.Error())
		return false
	}
	return true
}

// orEmpty sikrer at en nil-slice serialiseres som [] og ikke null.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrRefNotFound):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrGameFinalized):
		writeError(w, http.StatusConflict, "game is finalized")
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		log.Printf("store error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
