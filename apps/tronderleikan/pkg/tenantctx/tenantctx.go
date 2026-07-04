// Package tenantctx frakter tenant-ID gjennom request-context og inn i
// Postgres-transaksjonen for row-level security (SPEC §8).
//
// Mønsteret: Middleware legger tenant-ID i context per request, tjenesten
// åpner en tx og kaller SetLocal, og RLS-policyen filtrerer på
// current_setting('app.tenant_id'). App-laget filtrerer i tillegg selv -
// RLS er sikkerhetsnettet.
//
// Eksempel-RLS-policy for en domenetabell:
//
//	ALTER TABLE game ENABLE ROW LEVEL SECURITY;
//	ALTER TABLE game FORCE ROW LEVEL SECURITY; -- gjelder også tabelleieren
//	CREATE POLICY game_tenant_isolation ON game
//	    USING (tenant_id = current_setting('app.tenant_id')::uuid);
package tenantctx

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Header er request-headeren tenant-ID leses fra når path-param mangler.
const Header = "X-Tenant-ID"

// PathParam er navnet på path-parameteren Middleware leser tenant-ID fra,
// f.eks. mønsteret /api/competition/tenants/{tenant_id}/games.
const PathParam = "tenant_id"

type tenantKey struct{}

// With legger tenant-ID i context.
func With(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantKey{}, id)
}

// From henter tenant-ID fra context (satt av Middleware eller With).
func From(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(tenantKey{}).(uuid.UUID)
	return id, ok
}

// Middleware henter tenant-ID fra path-param {tenant_id} (std-mux 1.22+ og
// chi >= 5.1 populerer begge r.PathValue) eller X-Tenant-ID-headeren, og
// legger den i context. Mangler eller ugyldig UUID gir 400.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.PathValue(PathParam)
		if raw == "" {
			raw = r.Header.Get(Header)
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			http.Error(w, "missing or invalid tenant id", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r.WithContext(With(r.Context(), id)))
	})
}

// SetLocal setter app.tenant_id for gjeldende transaksjon, slik at
// RLS-policyene slår inn. set_config(..., true) tilsvarer SET LOCAL, men er
// parameteriserbar (SET LOCAL tar ikke bind-parametre).
func SetLocal(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) error {
	if _, err := tx.Exec(ctx,
		`SELECT set_config('app.tenant_id', $1, true)`, tenantID.String(),
	); err != nil {
		return fmt.Errorf("set app.tenant_id: %w", err)
	}
	return nil
}
