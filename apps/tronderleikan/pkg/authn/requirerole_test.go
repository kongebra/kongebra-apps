package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireRole(t *testing.T) {
	var called bool
	guarded := RequireRole(RoleOrganizer)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	cases := map[string]struct {
		principal *Principal
		wantCode  int
		wantCall  bool
	}{
		"ingen principal -> 401": {
			principal: nil,
			wantCode:  http.StatusUnauthorized,
		},
		"feil rolle -> 403": {
			principal: &Principal{UserID: "u", Roles: []string{RolePlayer}},
			wantCode:  http.StatusForbidden,
		},
		"riktig rolle -> 200": {
			principal: &Principal{UserID: "u", Roles: []string{RolePlayer, RoleOrganizer}},
			wantCode:  http.StatusOK,
			wantCall:  true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			if tc.principal != nil {
				req = req.WithContext(WithPrincipal(req.Context(), *tc.principal))
			}
			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode || called != tc.wantCall {
				t.Errorf("status=%d called=%v, vil ha %d/%v", rec.Code, called, tc.wantCode, tc.wantCall)
			}
		})
	}
}
