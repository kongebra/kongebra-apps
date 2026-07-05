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

// fakeStore implementerer Store; hver metode kan overstyres per test.
type fakeStore struct {
	createFn       func(ctx context.Context, tenantID uuid.UUID, in PersonInput) (Person, error)
	getFn          func(ctx context.Context, tenantID, id uuid.UUID) (Person, error)
	listFn         func(ctx context.Context, tenantID uuid.UUID) ([]Person, error)
	updateFn       func(ctx context.Context, tenantID, id uuid.UUID, in PersonInput) (Person, error)
	deleteFn       func(ctx context.Context, tenantID, id uuid.UUID) error
	setAccountFn   func(ctx context.Context, tenantID, id uuid.UUID, accountID string) (Person, error)
	clearAccountFn func(ctx context.Context, tenantID, id uuid.UUID) (Person, error)
}

func (f *fakeStore) Create(ctx context.Context, t uuid.UUID, in PersonInput) (Person, error) {
	return f.createFn(ctx, t, in)
}
func (f *fakeStore) Get(ctx context.Context, t, id uuid.UUID) (Person, error) {
	return f.getFn(ctx, t, id)
}
func (f *fakeStore) List(ctx context.Context, t uuid.UUID) ([]Person, error) { return f.listFn(ctx, t) }
func (f *fakeStore) Update(ctx context.Context, t, id uuid.UUID, in PersonInput) (Person, error) {
	return f.updateFn(ctx, t, id, in)
}
func (f *fakeStore) Delete(ctx context.Context, t, id uuid.UUID) error { return f.deleteFn(ctx, t, id) }
func (f *fakeStore) SetAccount(ctx context.Context, t, id uuid.UUID, a string) (Person, error) {
	return f.setAccountFn(ctx, t, id, a)
}
func (f *fakeStore) ClearAccount(ctx context.Context, t, id uuid.UUID) (Person, error) {
	return f.clearAccountFn(ctx, t, id)
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

func newTestAPI(store Store, public bool) http.Handler {
	a := &api{store: store, vis: fakeVisibility{public: public}, validator: fakeValidator{}}
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

const tenantPath = "/api/roster/tenants/11111111-1111-7111-8111-111111111111/persons"

var testTenant = uuid.MustParse("11111111-1111-7111-8111-111111111111")

// --- write-autorisasjon (SPEC §6: organizer kreves for skriv) ---

func TestWriteRequiresOrganizer(t *testing.T) {
	created := Person{ID: uuid.New(), TenantID: testTenant, Name: "Ada"}
	store := &fakeStore{createFn: func(context.Context, uuid.UUID, PersonInput) (Person, error) {
		return created, nil
	}}
	h := newTestAPI(store, true)

	cases := map[string]struct {
		token string
		want  int
	}{
		"uten token -> 401":    {"", http.StatusUnauthorized},
		"ugyldig token -> 401": {"tull", http.StatusUnauthorized},
		"player-rolle -> 403":  {"player", http.StatusForbidden},
		"organizer -> 201":     {"organizer", http.StatusCreated},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req(http.MethodPost, tenantPath, tc.token, `{"name":"Ada"}`))
			if rec.Code != tc.want {
				t.Errorf("status = %d, vil ha %d (body: %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// --- read-gate (SPEC §6: anonym lese hvis public_visibility på) ---

func TestReadGate(t *testing.T) {
	store := &fakeStore{listFn: func(context.Context, uuid.UUID) ([]Person, error) {
		return []Person{}, nil
	}}

	t.Run("anonym + offentlig -> 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newTestAPI(store, true).ServeHTTP(rec, req(http.MethodGet, tenantPath, "", ""))
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, vil ha 200", rec.Code)
		}
	})

	t.Run("anonym + ikke-offentlig -> 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newTestAPI(store, false).ServeHTTP(rec, req(http.MethodGet, tenantPath, "", ""))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, vil ha 401", rec.Code)
		}
	})

	t.Run("autentisert + ikke-offentlig -> 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newTestAPI(store, false).ServeHTTP(rec, req(http.MethodGet, tenantPath, "player", ""))
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, vil ha 200", rec.Code)
		}
	})

	t.Run("ugyldig token -> 401 selv om offentlig", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newTestAPI(store, true).ServeHTTP(rec, req(http.MethodGet, tenantPath, "tull", ""))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, vil ha 401", rec.Code)
		}
	})
}

// --- tenant fra path når inn i store-kallet ---

func TestTenantScopedFromPath(t *testing.T) {
	var gotTenant uuid.UUID
	store := &fakeStore{createFn: func(_ context.Context, tenantID uuid.UUID, _ PersonInput) (Person, error) {
		gotTenant = tenantID
		return Person{ID: uuid.New(), TenantID: tenantID, Name: "Ada"}, nil
	}}
	rec := httptest.NewRecorder()
	newTestAPI(store, true).ServeHTTP(rec, req(http.MethodPost, tenantPath, "organizer", `{"name":"Ada"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if gotTenant != testTenant {
		t.Errorf("tenant sendt til store = %s, vil ha %s", gotTenant, testTenant)
	}
}

// --- feilmapping ---

func TestErrorMapping(t *testing.T) {
	id := uuid.New()
	path := tenantPath + "/" + id.String()

	t.Run("not found -> 404", func(t *testing.T) {
		store := &fakeStore{getFn: func(context.Context, uuid.UUID, uuid.UUID) (Person, error) {
			return Person{}, ErrNotFound
		}}
		rec := httptest.NewRecorder()
		newTestAPI(store, true).ServeHTTP(rec, req(http.MethodGet, path, "", ""))
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, vil ha 404", rec.Code)
		}
	})

	t.Run("account taken -> 409", func(t *testing.T) {
		store := &fakeStore{setAccountFn: func(context.Context, uuid.UUID, uuid.UUID, string) (Person, error) {
			return Person{}, ErrAccountTaken
		}}
		rec := httptest.NewRecorder()
		newTestAPI(store, true).ServeHTTP(rec, req(http.MethodPut, path+"/account", "organizer", `{"account_id":"sub-1"}`))
		if rec.Code != http.StatusConflict {
			t.Errorf("status = %d, vil ha 409", rec.Code)
		}
	})

	t.Run("ugyldig json -> 400", func(t *testing.T) {
		store := &fakeStore{createFn: func(context.Context, uuid.UUID, PersonInput) (Person, error) {
			return Person{}, nil
		}}
		rec := httptest.NewRecorder()
		newTestAPI(store, true).ServeHTTP(rec, req(http.MethodPost, tenantPath, "organizer", `{"unknown":1}`))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, vil ha 400", rec.Code)
		}
	})

	t.Run("ugyldig person-id -> 400", func(t *testing.T) {
		store := &fakeStore{getFn: func(context.Context, uuid.UUID, uuid.UUID) (Person, error) {
			return Person{}, nil
		}}
		rec := httptest.NewRecorder()
		newTestAPI(store, true).ServeHTTP(rec, req(http.MethodGet, tenantPath+"/not-a-uuid", "", ""))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, vil ha 400", rec.Code)
		}
	})
}
