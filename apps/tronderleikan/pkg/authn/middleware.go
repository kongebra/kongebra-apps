package authn

import (
	"context"
	"net/http"
	"strings"
)

type principalKey struct{}

// WithPrincipal legger principalen i context.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom henter principalen fra context (satt av Middleware).
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// Middleware krever gyldig Bearer-token og legger Principal i request-context.
// Fungerer med både std net/http-mux og chi.
//
// ponytail: monteres kun på beskyttede ruter - anonym tilgang (offentlige
// scoreboards, SPEC §6) er ruter uten denne middlewaren. En optional-variant
// ("innlogget hvis mulig") legges til når web/BFF trenger den.
func (v *Validator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || raw == "" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		p, err := v.Validate(r.Context(), raw)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}

// RequireRole gir en middleware som krever at principalen (satt av en
// forutgående auth-middleware) har rollen (SPEC §6). Mangler principal -> 401,
// mangler rollen -> 403. Komponeres etter token-validering.
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFrom(r.Context())
			if !ok {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			if !p.HasRole(role) {
				http.Error(w, "requires role "+role, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
