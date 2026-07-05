package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/google/uuid"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/authn"
)

// Server binder HTTP-rutene til Service/Repo. Admin-plane-endepunktene er bak
// platform_admin-rollen (SPEC §6); slug-oppslaget er åpent (web trenger det for
// anonym tenant-utledning, SPEC §10).
type Server struct {
	svc       *Service
	repo      *Repo
	validator *authn.Validator
}

// NewServer lager en Server. validator brukes til å gate admin-rutene.
func NewServer(svc *Service, repo *Repo, validator *authn.Validator) *Server {
	return &Server{svc: svc, repo: repo, validator: validator}
}

// Handler bygger ruteren. Admin-ruter: validator.Middleware (krever token) +
// requirePlatformAdmin (krever rollen). Health + slug-oppslag er åpne.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)

	// Åpent: web utleder tenant fra slug uten token (SPEC §10).
	mux.HandleFunc("GET /api/platform/tenants/by-slug/{slug}", s.handleGetBySlug)

	// Admin-plane (platform_admin, SPEC §6, §7).
	admin := func(h http.HandlerFunc) http.Handler {
		return s.validator.Middleware(requirePlatformAdmin(h))
	}
	mux.Handle("POST /api/platform/tenants", admin(s.handleCreate))
	mux.Handle("GET /api/platform/tenants", admin(s.handleList))
	mux.Handle("GET /api/platform/tenants/{id}", admin(s.handleGet))
	mux.Handle("PATCH /api/platform/tenants/{id}", admin(s.handleUpdate))
	mux.Handle("DELETE /api/platform/tenants/{id}", admin(s.handleDelete))

	return mux
}

// requirePlatformAdmin krever at principalen (satt av validator.Middleware) har
// platform_admin-rollen. Uten den: 403.
func requirePlatformAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := authn.PrincipalFrom(r.Context())
		if !ok || !p.HasRole(authn.RolePlatformAdmin) {
			writeError(w, http.StatusForbidden, "krever platform_admin-rollen")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

// --- DTO-er ---

type adminDTO struct {
	Email      string `json:"email"`
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
	Password   string `json:"password"`
}

type createTenantRequest struct {
	Name             string   `json:"name"`
	Slug             string   `json:"slug"`
	PublicVisibility *bool    `json:"public_visibility"` // nil = default true
	Admin            adminDTO `json:"admin"`
}

type updateTenantRequest struct {
	Name             string `json:"name"`
	PublicVisibility bool   `json:"public_visibility"`
}

// publicTenant er den anonyme visningen (slug-oppslag): ingen zitadel_org_id.
type publicTenant struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	Slug             string    `json:"slug"`
	PublicVisibility bool      `json:"public_visibility"`
}

// --- handlere ---

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createTenantRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ugyldig JSON: "+err.Error())
		return
	}
	publicVisibility := true
	if req.PublicVisibility != nil {
		publicVisibility = *req.PublicVisibility
	}
	tenant, err := s.svc.CreateTenant(r.Context(), CreateTenantInput{
		Name:             req.Name,
		Slug:             req.Slug,
		PublicVisibility: publicVisibility,
		Admin: AdminSpec{
			Email:      req.Admin.Email,
			GivenName:  req.Admin.GivenName,
			FamilyName: req.Admin.FamilyName,
			Password:   req.Admin.Password,
		},
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tenant)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	tenants, err := s.repo.List(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if tenants == nil {
		tenants = []Tenant{}
	}
	writeJSON(w, http.StatusOK, tenants)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "ugyldig tenant-id")
		return
	}
	tenant, err := s.repo.GetByID(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tenant)
}

func (s *Server) handleGetBySlug(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	tenant, err := s.repo.GetBySlug(r.Context(), slug)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, publicTenant{
		ID:               tenant.ID,
		Name:             tenant.Name,
		Slug:             tenant.Slug,
		PublicVisibility: tenant.PublicVisibility,
	})
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "ugyldig tenant-id")
		return
	}
	var req updateTenantRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "ugyldig JSON: "+err.Error())
		return
	}
	if err := ValidateName(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tenant, err := s.repo.Update(r.Context(), id, req.Name, req.PublicVisibility)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tenant)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "ugyldig tenant-id")
		return
	}
	if err := s.repo.Delete(r.Context(), id); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeServiceError mapper domenefeil til HTTP-status.
func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "tenant finnes ikke")
	case errors.Is(err, ErrSlugTaken):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvalidSlug), errors.Is(err, ErrInvalidName), errors.Is(err, ErrInvalidAdmin):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("internal error: %v", err)
		writeError(w, http.StatusInternalServerError, "intern feil")
	}
}
