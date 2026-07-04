package main

import "testing"

func TestParseTarget(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    Target
		wantErr bool
	}{
		{name: "local http with port", in: "http://localhost:8300", want: Target{Domain: "localhost", Port: "8300", TLS: false}},
		{name: "cluster https default port", in: "https://auth.newb.no", want: Target{Domain: "auth.newb.no", Port: "443", TLS: true}},
		{name: "https custom port", in: "https://auth.example.com:8443", want: Target{Domain: "auth.example.com", Port: "8443", TLS: true}},
		{name: "http default port", in: "http://zitadel", want: Target{Domain: "zitadel", Port: "80", TLS: false}},
		{name: "trailing slash ok", in: "http://localhost:8300/", want: Target{Domain: "localhost", Port: "8300", TLS: false}},
		{name: "missing scheme", in: "localhost:8300", wantErr: true},
		{name: "unsupported scheme", in: "grpc://localhost:8300", wantErr: true},
		{name: "empty", in: "", wantErr: true},
		{name: "path rejected", in: "https://auth.newb.no/management", wantErr: true},
		{name: "query rejected", in: "https://auth.newb.no?foo=bar", wantErr: true},
		{name: "fragment rejected", in: "https://auth.newb.no#frag", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTarget(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseTarget(%q) = %+v, vil ha feil", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTarget(%q) uventet feil: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("parseTarget(%q) = %+v, vil ha %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveToken(t *testing.T) {
	readOK := func(content string) func(string) ([]byte, error) {
		return func(string) ([]byte, error) { return []byte(content), nil }
	}
	t.Run("inline", func(t *testing.T) {
		got, err := resolveToken(envFrom(map[string]string{EnvPAT: "tok"}), readOK(""))
		if err != nil || got != "tok" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("file trimmed", func(t *testing.T) {
		got, err := resolveToken(envFrom(map[string]string{EnvPATFile: "/x"}), readOK("  filetok\n"))
		if err != nil || got != "filetok" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("both set is error", func(t *testing.T) {
		_, err := resolveToken(envFrom(map[string]string{EnvPAT: "a", EnvPATFile: "/x"}), readOK("b"))
		if err == nil {
			t.Fatal("vil ha feil når begge er satt")
		}
	})
	t.Run("none set is error", func(t *testing.T) {
		_, err := resolveToken(envFrom(map[string]string{}), readOK(""))
		if err == nil {
			t.Fatal("vil ha feil når ingen er satt")
		}
	})
	t.Run("empty file is error", func(t *testing.T) {
		_, err := resolveToken(envFrom(map[string]string{EnvPATFile: "/x"}), readOK("   \n"))
		if err == nil {
			t.Fatal("vil ha feil når fila er tom")
		}
	})
}

func TestLoadConfigDefaultsAndValidation(t *testing.T) {
	t.Run("missing api url", func(t *testing.T) {
		_, err := LoadConfig(envFrom(map[string]string{EnvPAT: "t", EnvTestPassword: "p"}))
		if err == nil {
			t.Fatal("vil ha feil uten ZITADEL_API_URL")
		}
	})
	t.Run("missing password", func(t *testing.T) {
		_, err := LoadConfig(envFrom(map[string]string{EnvAPIURL: "http://localhost:8300", EnvPAT: "t"}))
		if err == nil {
			t.Fatal("vil ha feil uten SEED_TEST_PASSWORD")
		}
	})
	t.Run("defaults applied", func(t *testing.T) {
		cfg, err := LoadConfig(envFrom(map[string]string{
			EnvAPIURL:       "http://localhost:8300",
			EnvPAT:          "t",
			EnvTestPassword: "Password1!",
		}))
		if err != nil {
			t.Fatalf("uventet feil: %v", err)
		}
		if cfg.PlatformOrgName != defaultPlatformOrg || cfg.TenantOrgName != defaultTenantOrg || cfg.ProjectName != defaultProjectName {
			t.Fatalf("defaults ikke satt: %+v", cfg)
		}
		if len(cfg.Users) != 3 {
			t.Fatalf("vil ha 3 testbrukere, fikk %d", len(cfg.Users))
		}
	})
	t.Run("overrides applied", func(t *testing.T) {
		cfg, err := LoadConfig(envFrom(map[string]string{
			EnvAPIURL:       "https://auth.newb.no",
			EnvPAT:          "t",
			EnvTestPassword: "p",
			EnvProjectName:  "custom",
		}))
		if err != nil {
			t.Fatalf("uventet feil: %v", err)
		}
		if cfg.ProjectName != "custom" {
			t.Fatalf("override ikke brukt: %q", cfg.ProjectName)
		}
	})
}

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}
