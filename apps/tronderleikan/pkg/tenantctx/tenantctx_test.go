package tenantctx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestWithFrom(t *testing.T) {
	if _, ok := From(context.Background()); ok {
		t.Error("From fant tenant i tom context")
	}
	id := uuid.New()
	got, ok := From(With(context.Background(), id))
	if !ok || got != id {
		t.Errorf("From = %s/%v, vil ha %s/true", got, ok, id)
	}
}

func TestMiddleware(t *testing.T) {
	tenant := uuid.New()
	var got uuid.UUID
	var called bool
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = From(r.Context())
		called = true
	}))

	t.Run("path-param", func(t *testing.T) {
		called = false
		mux := http.NewServeMux()
		mux.Handle("/tenants/{tenant_id}/games", handler)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tenants/"+tenant.String()+"/games", nil))
		if rec.Code != http.StatusOK || !called || got != tenant {
			t.Errorf("status=%d called=%v got=%s, vil ha 200/true/%s", rec.Code, called, got, tenant)
		}
	})

	t.Run("header-fallback", func(t *testing.T) {
		called = false
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(Header, tenant.String())
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !called || got != tenant {
			t.Errorf("status=%d called=%v got=%s, vil ha 200/true/%s", rec.Code, called, got, tenant)
		}
	})

	t.Run("mangler eller ugyldig -> 400", func(t *testing.T) {
		for name, req := range map[string]*http.Request{
			"mangler": httptest.NewRequest(http.MethodGet, "/", nil),
			"ugyldig": func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Set(Header, "ikke-en-uuid")
				return r
			}(),
		} {
			called = false
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || called {
				t.Errorf("%s: status=%d called=%v, vil ha 400/false", name, rec.Code, called)
			}
		}
	})
}

// fakeTx fanger Exec-kall (kun Exec brukes; resten paniker via nil-embed).
type fakeTx struct {
	pgx.Tx
	sql  string
	args []any
}

func (t *fakeTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.sql, t.args = sql, args
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func TestSetLocal(t *testing.T) {
	tenant := uuid.New()
	tx := &fakeTx{}
	if err := SetLocal(context.Background(), tx, tenant); err != nil {
		t.Fatalf("SetLocal: %v", err)
	}
	// set_config(..., true) = SET LOCAL, parameterisert (RLS-kontrakten i SPEC §8)
	if !strings.Contains(tx.sql, "set_config('app.tenant_id', $1, true)") {
		t.Errorf("SQL = %q", tx.sql)
	}
	if len(tx.args) != 1 || tx.args[0] != tenant.String() {
		t.Errorf("args = %v, vil ha [%s]", tx.args, tenant)
	}
}
