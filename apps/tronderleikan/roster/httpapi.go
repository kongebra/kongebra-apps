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
}

// routes bygger muxen (SPEC §10: /api/roster/..., tenant-UUID i path).
// Skriv krever organizer-rolle (pkg/authn). Les er gatet av readGate:
// autentisert tenant-tilgang ELLER anonym hvis tenant har public_visibility på.
func (a *api) routes() http.Handler {
	mux := http.NewServeMux()

	// Skriv: gyldig token + organizer-rolle (SPEC §6). authMiddleware validerer
	// tokenet, pkg/authn.RequireRole håndhever rollen.
	write := func(h http.HandlerFunc) http.Handler {
		return tenantctx.Middleware(a.authMiddleware(authn.RequireRole(authn.RoleOrganizer)(h)))
	}
	// Les: autentisert eller anonym-hvis-offentlig.
	read := func(h http.HandlerFunc) http.Handler {
		return tenantctx.Middleware(a.readGate(h))
	}

	const base = "/api/roster/tenants/{tenant_id}/persons"
	mux.Handle("POST "+base, write(a.handleCreate))
	mux.Handle("GET "+base, read(a.handleList))
	mux.Handle("GET "+base+"/{id}", read(a.handleGet))
	mux.Handle("PUT "+base+"/{id}", write(a.handleUpdate))
	mux.Handle("DELETE "+base+"/{id}", write(a.handleDelete))
	mux.Handle("PUT "+base+"/{id}/account", write(a.handleSetAccount))
	mux.Handle("DELETE "+base+"/{id}/account", write(a.handleClearAccount))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})
	return mux
}

// authMiddleware validerer Bearer-tokenet og legger principalen i context, slik
// pkg/authn.RequireRole kan håndheve rollen etterpå. Bruker tokenValidator-
// interfacet (ikke *authn.Validator.Middleware direkte) fordi readGate trenger
// samme sømmen for optional-auth, og for at handler-testene skal kunne fake det.
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

// readGate slipper gjennom autentiserte kall, ellers anonym lesing hvis
// tenanten har public_visibility på (SPEC §6). Ugyldig token = 401.
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
		// Anonym: kun tillatt hvis tenanten er offentlig synlig.
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

// authenticate validerer Bearer-tokenet og returnerer principalen.
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

// --- handlers ---

func (a *api) handleCreate(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	var in PersonInput
	if !decodeJSON(w, r, &in) {
		return
	}
	p, err := a.store.Create(r.Context(), tenantID, in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (a *api) handleList(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	persons, err := a.store.List(r.Context(), tenantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if persons == nil {
		persons = []Person{}
	}
	writeJSON(w, http.StatusOK, persons)
}

func (a *api) handleGet(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := a.store.Get(r.Context(), tenantID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *api) handleUpdate(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in PersonInput
	if !decodeJSON(w, r, &in) {
		return
	}
	p, err := a.store.Update(r.Context(), tenantID, id, in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *api) handleDelete(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.Delete(r.Context(), tenantID, id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// accountRequest er payloaden for account-kobling (SPEC §4).
type accountRequest struct {
	AccountID string `json:"account_id"`
}

func (a *api) handleSetAccount(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req accountRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := a.store.SetAccount(r.Context(), tenantID, id, req.AccountID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *api) handleClearAccount(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := tenantctx.From(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := a.store.ClearAccount(r.Context(), tenantID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// --- helpers ---

func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid person id")
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

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "person not found")
	case errors.Is(err, ErrAccountTaken):
		writeError(w, http.StatusConflict, "account already linked to another person")
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
