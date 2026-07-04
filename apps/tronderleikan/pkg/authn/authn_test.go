package authn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testAudience = "tronderleikan-api"

// testIssuer er en falsk Zitadel: OIDC-discovery + JWKS + token-minting.
type testIssuer struct {
	srv  *httptest.Server
	keys map[string]*rsa.PrivateKey // kid -> nøkkel
}

func newTestIssuer(t *testing.T) *testIssuer {
	t.Helper()
	ti := &testIssuer{keys: map[string]*rsa.PrivateKey{}}
	ti.addKey(t, "key-1")

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"issuer":   ti.srv.URL,
			"jwks_uri": ti.srv.URL + "/oauth/v2/keys",
		})
	})
	mux.HandleFunc("/oauth/v2/keys", func(w http.ResponseWriter, r *http.Request) {
		var keys []map[string]string
		for kid, key := range ti.keys {
			pub := key.Public().(*rsa.PublicKey)
			keys = append(keys, map[string]string{
				"kid": kid,
				"kty": "RSA",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			})
		}
		json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	})
	ti.srv = httptest.NewServer(mux)
	t.Cleanup(ti.srv.Close)
	return ti
}

func (ti *testIssuer) addKey(t *testing.T, kid string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	ti.keys[kid] = key
}

// mint signerer et token med gitt kid og claims-overstyringer.
func (ti *testIssuer) mint(t *testing.T, kid string, override jwt.MapClaims) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":    ti.srv.URL,
		"sub":    "user-123",
		"aud":    testAudience,
		"exp":    time.Now().Add(time.Hour).Unix(),
		orgClaim: "org-456",
		rolesClaim: map[string]any{
			RoleOrganizer: map[string]any{"org-456": "org456.example.com"},
			RolePlayer:    map[string]any{"org-456": "org456.example.com"},
		},
	}
	for k, v := range override {
		if v == nil {
			delete(claims, k)
			continue
		}
		claims[k] = v
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	raw, err := tok.SignedString(ti.keys[kid])
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return raw
}

func newTestValidator(t *testing.T, ti *testIssuer) *Validator {
	t.Helper()
	t.Setenv(EnvIssuer, ti.srv.URL)
	v, err := NewValidator(context.Background(), testAudience)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	return v
}

func TestNewValidatorRequiresIssuerEnv(t *testing.T) {
	t.Setenv(EnvIssuer, "")
	if _, err := NewValidator(context.Background(), testAudience); err == nil {
		t.Fatal("NewValidator godtok manglende AUTH_ISSUER")
	}
}

func TestValidateHappyPath(t *testing.T) {
	ti := newTestIssuer(t)
	v := newTestValidator(t, ti)

	p, err := v.Validate(context.Background(), ti.mint(t, "key-1", nil))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.UserID != "user-123" || p.OrgID != "org-456" {
		t.Errorf("principal = %+v", p)
	}
	if len(p.Roles) != 2 || !p.HasRole(RoleOrganizer) || !p.HasRole(RolePlayer) {
		t.Errorf("roles = %v, vil ha [organizer player]", p.Roles)
	}
	if p.HasRole(RolePlatformAdmin) {
		t.Error("HasRole(platform_admin) = true for organizer-token")
	}
}

func TestValidateRejectsBadTokens(t *testing.T) {
	ti := newTestIssuer(t)
	v := newTestValidator(t, ti)

	otherIssuer := newTestIssuer(t) // annen nøkkel => ugyldig signatur
	otherKeyToken := otherIssuer.mint(t, "key-1", jwt.MapClaims{"iss": ti.srv.URL})

	cases := map[string]string{
		"utløpt":        ti.mint(t, "key-1", jwt.MapClaims{"exp": time.Now().Add(-time.Minute).Unix()}),
		"mangler exp":   ti.mint(t, "key-1", jwt.MapClaims{"exp": nil}),
		"feil issuer":   ti.mint(t, "key-1", jwt.MapClaims{"iss": "https://evil.example.com"}),
		"feil audience": ti.mint(t, "key-1", jwt.MapClaims{"aud": "annen-app"}),
		"mangler sub":   ti.mint(t, "key-1", jwt.MapClaims{"sub": nil}),
		"feil nøkkel":   otherKeyToken,
		"søppel":        "ikke.et.token",
	}
	for name, raw := range cases {
		if _, err := v.Validate(context.Background(), raw); err == nil {
			t.Errorf("%s: Validate godtok tokenet", name)
		}
	}
}

func TestValidateRefreshesJWKSOnUnknownKid(t *testing.T) {
	ti := newTestIssuer(t)
	v := newTestValidator(t, ti)
	v.minRefresh = 0 // skru av rate-limit i test

	ti.addKey(t, "key-2") // nøkkelrotasjon hos issuer etter init
	p, err := v.Validate(context.Background(), ti.mint(t, "key-2", nil))
	if err != nil {
		t.Fatalf("Validate etter rotasjon: %v", err)
	}
	if p.UserID != "user-123" {
		t.Errorf("principal = %+v", p)
	}
}

func TestValidateUnknownKidRateLimited(t *testing.T) {
	ti := newTestIssuer(t)
	v := newTestValidator(t, ti)
	// minRefresh er 1 min og JWKS ble nettopp hentet: ukjent kid skal feile
	// uten ny fetch
	ti.addKey(t, "key-3")
	if _, err := v.Validate(context.Background(), ti.mint(t, "key-3", nil)); err == nil {
		t.Fatal("Validate godtok token med ukjent kid innenfor rate-limit-vinduet")
	}
}

func TestMiddleware(t *testing.T) {
	ti := newTestIssuer(t)
	v := newTestValidator(t, ti)

	var got Principal
	var called bool
	handler := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, called = mustPrincipal(t, r)
	}))

	// uten token -> 401, handler ikke kalt
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized || called {
		t.Fatalf("uten token: status=%d called=%v, vil ha 401/false", rec.Code, called)
	}

	// ugyldig token -> 401
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer tull")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || called {
		t.Fatalf("ugyldig token: status=%d called=%v, vil ha 401/false", rec.Code, called)
	}

	// gyldig token -> handler kalt med principal i context
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+ti.mint(t, "key-1", nil))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("gyldig token: status=%d called=%v, vil ha 200/true", rec.Code, called)
	}
	if got.UserID != "user-123" {
		t.Errorf("principal i context = %+v", got)
	}
}

func mustPrincipal(t *testing.T, r *http.Request) (Principal, bool) {
	t.Helper()
	p, ok := PrincipalFrom(r.Context())
	if !ok {
		t.Fatal("principal mangler i context")
	}
	return p, true
}
