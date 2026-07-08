package main

import (
	"context"
	"fmt"

	"github.com/zitadel/zitadel-go/v3/pkg/client"
	adminpb "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/admin"
	apppb "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/app"
	managementpb "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/management"
	objectpb "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/object"
	objectv2pb "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/object/v2"
	orgpb "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/org"
	projectpb "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/project"
	userv2pb "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user/v2"
	"github.com/zitadel/zitadel-go/v3/pkg/zitadel"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// orgIDHeader lar en IAM_OWNER-token operere i en spesifikk org-kontekst.
// Zitadel leser resource-owner fra denne gRPC-metadata-headeren.
const orgIDHeader = "x-zitadel-orgid"

// zitadelDirectory er Directory-adapteren mot zitadel-go v3. Den er bevisst
// tynn: all beslutningslogikk (sjekk-før-opprett) ligger i Seeder. Denne fila
// er derfor ikke enhetstestet (krever levende Zitadel); den verifiseres i
// runtime-idempotenskjøringen (se README).
type zitadelDirectory struct {
	api *client.Client
}

// newZitadelDirectory kobler til Zitadel med PAT-auth (machine-user, IAM_OWNER).
func newZitadelDirectory(ctx context.Context, target Target, token string) (*zitadelDirectory, error) {
	opts := []zitadel.Option{}
	switch {
	case !target.TLS:
		opts = append(opts, zitadel.WithInsecure(target.Port))
	case target.Port != "" && target.Port != "443":
		var p uint16
		if _, err := fmt.Sscanf(target.Port, "%d", &p); err != nil {
			return nil, fmt.Errorf("ugyldig port %q: %w", target.Port, err)
		}
		opts = append(opts, zitadel.WithPort(p))
	}

	api, err := client.New(ctx, zitadel.New(target.Domain, opts...), client.WithAuth(client.PAT(token)))
	if err != nil {
		return nil, fmt.Errorf("koble til Zitadel: %w", err)
	}
	return &zitadelDirectory{api: api}, nil
}

func (d *zitadelDirectory) Close() error { return d.api.Close() }

// inOrg setter org-kontekst-headeren for kall som er org-scopet.
func inOrg(ctx context.Context, orgID string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, orgIDHeader, orgID)
}

func isAlreadyExists(err error) bool {
	return status.Code(err) == codes.AlreadyExists
}

func (d *zitadelDirectory) FindOrgByName(ctx context.Context, name string) (string, bool, error) {
	resp, err := d.api.AdminService().ListOrgs(ctx, &adminpb.ListOrgsRequest{
		Queries: []*orgpb.OrgQuery{{
			Query: &orgpb.OrgQuery_NameQuery{NameQuery: &orgpb.OrgNameQuery{
				Name:   name,
				Method: objectpb.TextQueryMethod_TEXT_QUERY_METHOD_EQUALS,
			}},
		}},
	})
	if err != nil {
		return "", false, fmt.Errorf("list orgs: %w", err)
	}
	for _, o := range resp.GetResult() {
		if o.GetName() == name {
			return o.GetId(), true, nil
		}
	}
	return "", false, nil
}

func (d *zitadelDirectory) CreateOrg(ctx context.Context, name string) (string, error) {
	resp, err := d.api.ManagementService().AddOrg(ctx, &managementpb.AddOrgRequest{Name: name})
	if err != nil {
		return "", fmt.Errorf("add org: %w", err)
	}
	return resp.GetId(), nil
}

func (d *zitadelDirectory) FindProjectByName(ctx context.Context, orgID, name string) (string, bool, error) {
	resp, err := d.api.ManagementService().ListProjects(inOrg(ctx, orgID), &managementpb.ListProjectsRequest{
		Queries: []*projectpb.ProjectQuery{{
			Query: &projectpb.ProjectQuery_NameQuery{NameQuery: &projectpb.ProjectNameQuery{
				Name:   name,
				Method: objectpb.TextQueryMethod_TEXT_QUERY_METHOD_EQUALS,
			}},
		}},
	})
	if err != nil {
		return "", false, fmt.Errorf("list projects: %w", err)
	}
	for _, p := range resp.GetResult() {
		if p.GetName() == name {
			return p.GetId(), true, nil
		}
	}
	return "", false, nil
}

func (d *zitadelDirectory) CreateProject(ctx context.Context, orgID, name string) (string, error) {
	resp, err := d.api.ManagementService().AddProject(inOrg(ctx, orgID), &managementpb.AddProjectRequest{
		Name: name,
		// roles må assertes/sjekkes for at JWT skal bære project-roles og for at
		// tilgang krever eksplisitt grant (SPEC §5/§6).
		ProjectRoleAssertion: true,
		ProjectRoleCheck:     true,
	})
	if err != nil {
		return "", fmt.Errorf("add project: %w", err)
	}
	return resp.GetId(), nil
}

func (d *zitadelDirectory) EnsureProjectRole(ctx context.Context, orgID, projectID string, role RoleDef) error {
	_, err := d.api.ManagementService().AddProjectRole(inOrg(ctx, orgID), &managementpb.AddProjectRoleRequest{
		ProjectId:   projectID,
		RoleKey:     role.Key,
		DisplayName: role.DisplayName,
		Group:       role.Group,
	})
	if err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("add project role: %w", err)
	}
	return nil
}

func (d *zitadelDirectory) FindProjectGrant(ctx context.Context, ownerOrgID, projectID, grantedOrgID string) (string, []string, bool, error) {
	resp, err := d.api.ManagementService().ListProjectGrants(inOrg(ctx, ownerOrgID), &managementpb.ListProjectGrantsRequest{
		ProjectId: projectID,
	})
	if err != nil {
		return "", nil, false, fmt.Errorf("list project grants: %w", err)
	}
	for _, g := range resp.GetResult() {
		if g.GetGrantedOrgId() == grantedOrgID {
			return g.GetGrantId(), g.GetGrantedRoleKeys(), true, nil
		}
	}
	return "", nil, false, nil
}

func (d *zitadelDirectory) UpdateProjectGrant(ctx context.Context, ownerOrgID, projectID, grantID string, roleKeys []string) error {
	_, err := d.api.ManagementService().UpdateProjectGrant(inOrg(ctx, ownerOrgID), &managementpb.UpdateProjectGrantRequest{
		ProjectId: projectID,
		GrantId:   grantID,
		RoleKeys:  roleKeys,
	})
	if err != nil {
		return fmt.Errorf("update project grant: %w", err)
	}
	return nil
}

func (d *zitadelDirectory) CreateProjectGrant(ctx context.Context, ownerOrgID, projectID, grantedOrgID string, roleKeys []string) (string, error) {
	resp, err := d.api.ManagementService().AddProjectGrant(inOrg(ctx, ownerOrgID), &managementpb.AddProjectGrantRequest{
		ProjectId:    projectID,
		GrantedOrgId: grantedOrgID,
		RoleKeys:     roleKeys,
	})
	if err != nil {
		return "", fmt.Errorf("add project grant: %w", err)
	}
	return resp.GetGrantId(), nil
}

func (d *zitadelDirectory) FindUserByEmail(ctx context.Context, orgID, email string) (string, bool, error) {
	resp, err := d.api.UserServiceV2().ListUsers(ctx, &userv2pb.ListUsersRequest{
		Queries: []*userv2pb.SearchQuery{
			{Query: &userv2pb.SearchQuery_OrganizationIdQuery{OrganizationIdQuery: &userv2pb.OrganizationIdQuery{
				OrganizationId: orgID,
			}}},
			{Query: &userv2pb.SearchQuery_EmailQuery{EmailQuery: &userv2pb.EmailQuery{
				EmailAddress: email,
				Method:       objectv2pb.TextQueryMethod_TEXT_QUERY_METHOD_EQUALS,
			}}},
		},
	})
	if err != nil {
		return "", false, fmt.Errorf("list users: %w", err)
	}
	for _, u := range resp.GetResult() {
		return u.GetUserId(), true, nil
	}
	return "", false, nil
}

func (d *zitadelDirectory) CreateUser(ctx context.Context, orgID string, u UserSpec, password string) (string, error) {
	resp, err := d.api.UserServiceV2().AddHumanUser(ctx, &userv2pb.AddHumanUserRequest{
		Organization: &objectv2pb.Organization{Org: &objectv2pb.Organization_OrgId{OrgId: orgID}},
		Profile: &userv2pb.SetHumanProfile{
			GivenName:  u.GivenName,
			FamilyName: u.FamilyName,
		},
		Email: &userv2pb.SetHumanEmail{
			Email:        u.Email,
			Verification: &userv2pb.SetHumanEmail_IsVerified{IsVerified: true},
		},
		PasswordType: &userv2pb.AddHumanUserRequest_Password{Password: &userv2pb.Password{
			Password:       password,
			ChangeRequired: false,
		}},
	})
	if err != nil {
		return "", fmt.Errorf("add human user: %w", err)
	}
	return resp.GetUserId(), nil
}

func (d *zitadelDirectory) EnsureUserGrant(ctx context.Context, orgID, userID, projectID, projectGrantID string, roleKeys []string) error {
	_, err := d.api.ManagementService().AddUserGrant(inOrg(ctx, orgID), &managementpb.AddUserGrantRequest{
		UserId:         userID,
		ProjectId:      projectID,
		ProjectGrantId: projectGrantID,
		RoleKeys:       roleKeys,
	})
	if err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("add user grant: %w", err)
	}
	return nil
}

func (d *zitadelDirectory) FindOIDCApp(ctx context.Context, orgID, projectID, name string) (string, string, []string, bool, error) {
	resp, err := d.api.ManagementService().ListApps(inOrg(ctx, orgID), &managementpb.ListAppsRequest{
		ProjectId: projectID,
		Queries: []*apppb.AppQuery{{
			Query: &apppb.AppQuery_NameQuery{NameQuery: &apppb.AppNameQuery{
				Name:   name,
				Method: objectpb.TextQueryMethod_TEXT_QUERY_METHOD_EQUALS,
			}},
		}},
	})
	if err != nil {
		return "", "", nil, false, fmt.Errorf("list apps: %w", err)
	}
	for _, a := range resp.GetResult() {
		if a.GetName() != name {
			continue
		}
		oidc := a.GetOidcConfig()
		return a.GetId(), oidc.GetClientId(), oidc.GetRedirectUris(), true, nil
	}
	return "", "", nil, false, nil
}

func (d *zitadelDirectory) CreateOIDCApp(ctx context.Context, orgID, projectID string, spec OIDCAppSpec) (string, string, error) {
	resp, err := d.api.ManagementService().AddOIDCApp(inOrg(ctx, orgID), oidcAppRequest(projectID, spec))
	if err != nil {
		return "", "", fmt.Errorf("add oidc app: %w", err)
	}
	return resp.GetAppId(), resp.GetClientId(), nil
}

func (d *zitadelDirectory) UpdateOIDCApp(ctx context.Context, orgID, projectID, appID string, spec OIDCAppSpec) error {
	_, err := d.api.ManagementService().UpdateOIDCAppConfig(inOrg(ctx, orgID), &managementpb.UpdateOIDCAppConfigRequest{
		ProjectId:                projectID,
		AppId:                    appID,
		RedirectUris:             spec.RedirectURIs,
		PostLogoutRedirectUris:   spec.PostLogoutURIs,
		ResponseTypes:            []apppb.OIDCResponseType{apppb.OIDCResponseType_OIDC_RESPONSE_TYPE_CODE},
		GrantTypes:               []apppb.OIDCGrantType{apppb.OIDCGrantType_OIDC_GRANT_TYPE_AUTHORIZATION_CODE, apppb.OIDCGrantType_OIDC_GRANT_TYPE_REFRESH_TOKEN},
		AppType:                  apppb.OIDCAppType_OIDC_APP_TYPE_WEB,
		AuthMethodType:           apppb.OIDCAuthMethodType_OIDC_AUTH_METHOD_TYPE_NONE,
		AccessTokenType:          apppb.OIDCTokenType_OIDC_TOKEN_TYPE_JWT,
		AccessTokenRoleAssertion: true,
		DevMode:                  false,
	})
	if err != nil {
		return fmt.Errorf("update oidc app: %w", err)
	}
	return nil
}

// oidcAppRequest builds a public PKCE web-app-request (SPEC section 10, Phase 2b plan
// Global Constraints). Auth method NONE = no client secret; Code + PKCE.
func oidcAppRequest(projectID string, spec OIDCAppSpec) *managementpb.AddOIDCAppRequest {
	return &managementpb.AddOIDCAppRequest{
		ProjectId:                projectID,
		Name:                     spec.Name,
		RedirectUris:             spec.RedirectURIs,
		PostLogoutRedirectUris:   spec.PostLogoutURIs,
		ResponseTypes:            []apppb.OIDCResponseType{apppb.OIDCResponseType_OIDC_RESPONSE_TYPE_CODE},
		GrantTypes:               []apppb.OIDCGrantType{apppb.OIDCGrantType_OIDC_GRANT_TYPE_AUTHORIZATION_CODE, apppb.OIDCGrantType_OIDC_GRANT_TYPE_REFRESH_TOKEN},
		AppType:                  apppb.OIDCAppType_OIDC_APP_TYPE_WEB,
		AuthMethodType:           apppb.OIDCAuthMethodType_OIDC_AUTH_METHOD_TYPE_NONE,
		Version:                  apppb.OIDCVersion_OIDC_VERSION_1_0,
		AccessTokenType:          apppb.OIDCTokenType_OIDC_TOKEN_TYPE_JWT,
		AccessTokenRoleAssertion: true,
		DevMode:                  false,
	}
}

var _ Directory = (*zitadelDirectory)(nil)
