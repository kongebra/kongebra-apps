// Package authn validerer Zitadel-utstedte JWT-er (SPEC §5, §6).
//
// Issuer-regelen (SPEC §5, ufravikelig): issuer/domenet hardkodes ALDRI -
// det leses fra env-varen AUTH_ISSUER. JWKS hentes via OIDC-discovery fra
// issueren. Validatoren sjekker signatur, issuer, expiry og audience, og
// ekstraherer Zitadel project-roles til en Principal.
package authn

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// EnvIssuer er env-varen issueren ALLTID leses fra (SPEC §5 issuer-regel).
const EnvIssuer = "AUTH_ISSUER"

// Zitadel-claims: project-roles er map[rolle]map[orgID]orgDomain,
// resourceowner er org-en brukeren tilhører.
const (
	rolesClaim = "urn:zitadel:iam:org:project:roles"
	orgClaim   = "urn:zitadel:iam:user:resourceowner:id"
)

// Project-roles definert i Zitadel-prosjektet tronderleikan (SPEC §6).
const (
	RolePlayer        = "player"
	RoleOrganizer     = "organizer"
	RoleTenantAdmin   = "tenant_admin"
	RolePlatformAdmin = "platform_admin"
)

// Principal er den autentiserte brukeren slik tjenestene ser den.
type Principal struct {
	UserID string   // Zitadel sub
	OrgID  string   // Zitadel resourceowner (tenant-org)
	Roles  []string // project-roles, sortert
}

// HasRole sier om principalen har rollen.
func (p Principal) HasRole(role string) bool {
	return slices.Contains(p.Roles, role)
}

// Validator validerer JWT-er mot issuerens JWKS.
type Validator struct {
	issuer   string
	audience string
	jwksURI  string
	client   *http.Client

	mu         sync.Mutex
	keys       map[string]*rsa.PublicKey
	lastFetch  time.Time
	minRefresh time.Duration // rate-limit for re-fetch ved ukjent kid
}

// NewValidator leser issuer fra AUTH_ISSUER (feiler om den mangler), gjør
// OIDC-discovery mot issueren og henter JWKS. audience er client/project-ID-en
// tokenet skal være utstedt for.
func NewValidator(ctx context.Context, audience string) (*Validator, error) {
	issuer := os.Getenv(EnvIssuer)
	if issuer == "" {
		return nil, fmt.Errorf("env %s må være satt - issuer hardkodes aldri (SPEC §5)", EnvIssuer)
	}
	if audience == "" {
		return nil, errors.New("audience må være satt")
	}
	v := &Validator{
		issuer:     strings.TrimSuffix(issuer, "/"),
		audience:   audience,
		client:     &http.Client{Timeout: 10 * time.Second},
		keys:       map[string]*rsa.PublicKey{},
		minRefresh: time.Minute,
	}
	if err := v.discover(ctx); err != nil {
		return nil, err
	}
	if err := v.refreshKeys(ctx); err != nil {
		return nil, err
	}
	return v, nil
}

// Validate sjekker signatur, issuer, expiry og audience, og returnerer
// principalen fra tokenets claims.
func (v *Validator) Validate(ctx context.Context, rawToken string) (Principal, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(rawToken, claims,
		func(t *jwt.Token) (any, error) { return v.keyFor(ctx, t) },
		// ponytail: kun RS256 - det Zitadel signerer med. Flere algoritmer
		// (ES256 etc.) = utvid listen + jwksKey-parsingen når behovet finnes.
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return Principal{}, fmt.Errorf("validate token: %w", err)
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return Principal{}, errors.New("token mangler sub")
	}
	orgID, _ := claims[orgClaim].(string)
	p := Principal{UserID: sub, OrgID: orgID}
	if roles, ok := claims[rolesClaim].(map[string]any); ok {
		for role := range roles {
			p.Roles = append(p.Roles, role)
		}
		slices.Sort(p.Roles)
	}
	return p, nil
}

// keyFor slår opp signeringsnøkkelen for tokenets kid. Ukjent kid utløser
// én rate-limitet JWKS-refresh (nøkkelrotasjon hos Zitadel).
func (v *Validator) keyFor(ctx context.Context, t *jwt.Token) (*rsa.PublicKey, error) {
	kid, _ := t.Header["kid"].(string)
	if kid == "" {
		return nil, errors.New("token mangler kid")
	}
	v.mu.Lock()
	key, ok := v.keys[kid]
	stale := time.Since(v.lastFetch) >= v.minRefresh
	v.mu.Unlock()
	if ok {
		return key, nil
	}
	if !stale {
		return nil, fmt.Errorf("ukjent kid %q", kid)
	}
	if err := v.refreshKeys(ctx); err != nil {
		return nil, err
	}
	v.mu.Lock()
	key, ok = v.keys[kid]
	v.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("ukjent kid %q etter JWKS-refresh", kid)
	}
	return key, nil
}

// discover henter jwks_uri fra issuerens OIDC-discovery-dokument og
// verifiserer at dokumentets issuer matcher den konfigurerte.
func (v *Validator) discover(ctx context.Context) error {
	var doc struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := v.getJSON(ctx, v.issuer+"/.well-known/openid-configuration", &doc); err != nil {
		return fmt.Errorf("oidc discovery: %w", err)
	}
	if strings.TrimSuffix(doc.Issuer, "/") != v.issuer {
		return fmt.Errorf("discovery-issuer %q matcher ikke konfigurert issuer %q", doc.Issuer, v.issuer)
	}
	if doc.JWKSURI == "" {
		return errors.New("oidc discovery: mangler jwks_uri")
	}
	v.jwksURI = doc.JWKSURI
	return nil
}

// refreshKeys henter JWKS og bygger kid -> nøkkel-mapet på nytt.
func (v *Validator) refreshKeys(ctx context.Context) error {
	var doc struct {
		Keys []jwksKey `json:"keys"`
	}
	if err := v.getJSON(ctx, v.jwksURI, &doc); err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		pub, err := k.publicKey()
		if err != nil {
			continue // hopp over ikke-RSA/ugyldige nøkler
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return errors.New("jwks inneholder ingen brukbare RSA-nøkler")
	}
	v.mu.Lock()
	v.keys = keys
	v.lastFetch = time.Now()
	v.mu.Unlock()
	return nil
}

func (v *Validator) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// jwksKey er en enkelt nøkkel i JWKS-dokumentet (kun RSA-feltene).
type jwksKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (k jwksKey) publicKey() (*rsa.PublicKey, error) {
	if k.Kty != "RSA" {
		return nil, fmt.Errorf("ustøttet kty %q", k.Kty)
	}
	n, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	e, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(n),
		E: int(new(big.Int).SetBytes(e).Int64()),
	}, nil
}
