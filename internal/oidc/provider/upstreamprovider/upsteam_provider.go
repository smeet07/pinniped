// Copyright 2020-2023 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package upstreamprovider

import (
	"context"
	"net/url"

	"golang.org/x/oauth2"
	"k8s.io/apimachinery/pkg/types"

	"go.pinniped.dev/internal/authenticators"
	"go.pinniped.dev/pkg/oidcclient/nonce"
	"go.pinniped.dev/pkg/oidcclient/oidctypes"
	"go.pinniped.dev/pkg/oidcclient/pkce"
)

type RevocableTokenType string

// These strings correspond to the token types defined by https://datatracker.ietf.org/doc/html/rfc7009#section-2.1
const (
	RefreshTokenType RevocableTokenType = "refresh_token"
	AccessTokenType  RevocableTokenType = "access_token"
)

// RefreshAttributes contains information about the user from the original login request
// and previous refreshes.
type RefreshAttributes struct {
	Username             string
	Subject              string
	DN                   string
	Groups               []string
	AdditionalAttributes map[string]string
	GrantedScopes        []string
}

type UpstreamOIDCIdentityProviderI interface {
	// GetName returns a name for this upstream provider. The controller watching the OIDCIdentityProviders will
	// set this to be the Name of the CR from its metadata. Note that this is different from the DisplayName configured
	// in each FederationDomain that uses this provider, so this name is for internal use only, not for interacting
	// with clients. Clients should not expect to see this name or send this name.
	GetName() string

	// GetClientID returns the OAuth client ID registered with the upstream provider to be used in the authorization code flow.
	GetClientID() string

	// GetResourceUID returns the Kubernetes resource ID
	GetResourceUID() types.UID

	// GetAuthorizationURL returns the Authorization Endpoint fetched from discovery.
	GetAuthorizationURL() *url.URL

	// HasUserInfoURL returns whether there is a non-empty value for userinfo_endpoint fetched from discovery.
	HasUserInfoURL() bool

	// GetScopes returns the scopes to request in authorization (authcode or password grant) flow.
	GetScopes() []string

	// GetUsernameClaim returns the ID Token username claim name. May return empty string, in which case we
	// will use some reasonable defaults.
	GetUsernameClaim() string

	// GetGroupsClaim returns the ID Token groups claim name. May return empty string, in which case we won't
	// try to read groups from the upstream provider.
	GetGroupsClaim() string

	// AllowsPasswordGrant returns true if a client should be allowed to use the resource owner password credentials grant
	// flow with this upstream provider. When false, it should not be allowed.
	AllowsPasswordGrant() bool

	// GetAdditionalAuthcodeParams returns additional params to be sent on authcode requests.
	GetAdditionalAuthcodeParams() map[string]string

	// GetAdditionalClaimMappings returns additional claims to be mapped from the upstream ID token.
	GetAdditionalClaimMappings() map[string]string

	// PasswordCredentialsGrantAndValidateTokens performs upstream OIDC resource owner password credentials grant and
	// token validation. Returns the validated raw tokens as well as the parsed claims of the ID token.
	PasswordCredentialsGrantAndValidateTokens(ctx context.Context, username, password string) (*oidctypes.Token, error)

	// ExchangeAuthcodeAndValidateTokens performs upstream OIDC authorization code exchange and token validation.
	// Returns the validated raw tokens as well as the parsed claims of the ID token.
	ExchangeAuthcodeAndValidateTokens(
		ctx context.Context,
		authcode string,
		pkceCodeVerifier pkce.Code,
		expectedIDTokenNonce nonce.Nonce,
		redirectURI string,
	) (*oidctypes.Token, error)

	// PerformRefresh will call the provider's token endpoint to perform a refresh grant. The provider may or may not
	// return a new ID or refresh token in the response. If it returns an ID token, then use ValidateToken to
	// validate the ID token.
	PerformRefresh(ctx context.Context, refreshToken string) (*oauth2.Token, error)

	// RevokeToken will attempt to revoke the given token, if the provider has a revocation endpoint.
	// It may return an error wrapped by a RetryableRevocationError, which is an error indicating that it may
	// be worth trying to revoke the same token again later. Any other error returned should be assumed to
	// represent an error such that it is not worth retrying revocation later, even though revocation failed.
	RevokeToken(ctx context.Context, token string, tokenType RevocableTokenType) error

	// ValidateTokenAndMergeWithUserInfo will validate the ID token. It will also merge the claims from the userinfo endpoint response
	// into the ID token's claims, if the provider offers the userinfo endpoint. It returns the validated/updated
	// tokens, or an error.
	ValidateTokenAndMergeWithUserInfo(ctx context.Context, tok *oauth2.Token, expectedIDTokenNonce nonce.Nonce, requireIDToken bool, requireUserInfo bool) (*oidctypes.Token, error)
}

type UpstreamLDAPIdentityProviderI interface {
	// GetName returns a name for this upstream provider.
	GetName() string

	// GetURL returns a URL which uniquely identifies this LDAP provider, e.g. "ldaps://host.example.com:1234".
	// This URL is not used for connecting to the provider, but rather is used for creating a globally unique user
	// identifier by being combined with the user's UID, since user UIDs are only unique within one provider.
	GetURL() *url.URL

	// GetResourceUID returns the Kubernetes resource ID
	GetResourceUID() types.UID

	// UserAuthenticator adds an interface method for performing user authentication against the upstream LDAP provider.
	authenticators.UserAuthenticator

	// PerformRefresh performs a refresh against the upstream LDAP identity provider
	PerformRefresh(ctx context.Context, storedRefreshAttributes RefreshAttributes) (groups []string, err error)
}
