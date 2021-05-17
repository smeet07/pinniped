// Copyright 2021 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/user"

	"go.pinniped.dev/internal/upstreamldap"
	"go.pinniped.dev/test/library"
)

func TestLDAPSearch(t *testing.T) {
	env := library.IntegrationEnv(t)

	// Note that these tests depend on the values hard-coded in the LDIF file in test/deploy/tools/ldap.yaml.
	// It requires the test LDAP server from the tools deployment.
	if len(env.ToolsNamespace) == 0 {
		t.Skip("Skipping test because it requires the test LDAP server in the tools namespace.")
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancelFunc() // this will send SIGKILL to the subprocess, just in case
	})

	hostPorts := findRecentlyUnusedLocalhostPorts(t, 2)
	ldapHostPort := hostPorts[0]
	unusedHostPort := hostPorts[1]

	// Expose the the test LDAP server's TLS port on the localhost.
	startKubectlPortForward(ctx, t, ldapHostPort, "ldaps", "ldap", env.ToolsNamespace)

	providerConfig := func(editFunc func(p *upstreamldap.ProviderConfig)) *upstreamldap.ProviderConfig {
		providerConfig := defaultProviderConfig(env, ldapHostPort)
		if editFunc != nil {
			editFunc(providerConfig)
		}
		return providerConfig
	}

	pinnyPassword := env.SupervisorUpstreamLDAP.TestUserPassword

	tests := []struct {
		name                string
		username            string
		password            string
		provider            *upstreamldap.Provider
		wantError           string
		wantAuthResponse    *authenticator.Response
		wantUnauthenticated bool
	}{
		{
			name:     "happy path",
			username: "pinny",
			password: pinnyPassword,
			provider: upstreamldap.New(*providerConfig(nil)),
			wantAuthResponse: &authenticator.Response{
				User: &user.DefaultInfo{Name: "pinny", UID: "1000", Groups: []string{}},
			},
		},
		{
			name:     "using a different user search base",
			username: "pinny",
			password: pinnyPassword,
			provider: upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.Base = "dc=pinniped,dc=dev" })),
			wantAuthResponse: &authenticator.Response{
				User: &user.DefaultInfo{Name: "pinny", UID: "1000", Groups: []string{}},
			},
		},
		{
			name:     "when the user search filter is already wrapped by parenthesis",
			username: "pinny",
			password: pinnyPassword,
			provider: upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.Filter = "(cn={})" })),
			wantAuthResponse: &authenticator.Response{
				User: &user.DefaultInfo{Name: "pinny", UID: "1000", Groups: []string{}},
			},
		},
		{
			name:     "when the UsernameAttribute is dn and a user search filter is provided",
			username: "pinny",
			password: pinnyPassword,
			provider: upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) {
				p.UserSearch.UsernameAttribute = "dn"
				p.UserSearch.Filter = "cn={}"
			})),
			wantAuthResponse: &authenticator.Response{
				User: &user.DefaultInfo{Name: "cn=pinny,ou=users,dc=pinniped,dc=dev", UID: "1000", Groups: []string{}},
			},
		},
		{
			name:     "when the user search filter allows for different ways of logging in and the first one is used",
			username: "pinny",
			password: pinnyPassword,
			provider: upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) {
				p.UserSearch.Filter = "(|(cn={})(mail={}))"
			})),
			wantAuthResponse: &authenticator.Response{
				User: &user.DefaultInfo{Name: "pinny", UID: "1000", Groups: []string{}},
			},
		},
		{
			name:     "when the user search filter allows for different ways of logging in and the second one is used",
			username: "pinny.ldap@example.com",
			password: pinnyPassword,
			provider: upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) {
				p.UserSearch.Filter = "(|(cn={})(mail={}))"
			})),
			wantAuthResponse: &authenticator.Response{
				User: &user.DefaultInfo{Name: "pinny", UID: "1000", Groups: []string{}},
			},
		},
		{
			name:     "when the UIDAttribute is dn",
			username: "pinny",
			password: pinnyPassword,
			provider: upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.UIDAttribute = "dn" })),
			wantAuthResponse: &authenticator.Response{
				User: &user.DefaultInfo{Name: "pinny", UID: "cn=pinny,ou=users,dc=pinniped,dc=dev", Groups: []string{}},
			},
		},
		{
			name:     "when the UIDAttribute is sn",
			username: "pinny",
			password: pinnyPassword,
			provider: upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.UIDAttribute = "sn" })),
			wantAuthResponse: &authenticator.Response{
				User: &user.DefaultInfo{Name: "pinny", UID: "Seal", Groups: []string{}},
			},
		},
		{
			name:     "when the UsernameAttribute is sn",
			username: "seAl", // note that this is not case-sensitive! sn=Seal. The server decides which fields are compared case-sensitive.
			password: pinnyPassword,
			provider: upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.UsernameAttribute = "sn" })),
			wantAuthResponse: &authenticator.Response{
				User: &user.DefaultInfo{Name: "Seal", UID: "1000", Groups: []string{}}, // note that the final answer has case preserved from the entry
			},
		},
		{
			name:     "when the UsernameAttribute is dn and there is no user search filter provided",
			username: "cn=pinny,ou=users,dc=pinniped,dc=dev",
			password: pinnyPassword,
			provider: upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) {
				p.UserSearch.UsernameAttribute = "dn"
				p.UserSearch.Filter = ""
			})),
			wantError: `must specify UserSearch Filter when UserSearch UsernameAttribute is "dn"`,
		},
		{
			name:      "when the bind user username is not a valid DN",
			username:  "pinny",
			password:  pinnyPassword,
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.BindUsername = "invalid-dn" })),
			wantError: `error binding as "invalid-dn" before user search: LDAP Result Code 34 "Invalid DN Syntax": invalid DN`,
		},
		{
			name:      "when the bind user username is wrong",
			username:  "pinny",
			password:  pinnyPassword,
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.BindUsername = "cn=wrong,dc=pinniped,dc=dev" })),
			wantError: `error binding as "cn=wrong,dc=pinniped,dc=dev" before user search: LDAP Result Code 49 "Invalid Credentials": `,
		},
		{
			name:      "when the bind user password is wrong",
			username:  "pinny",
			password:  pinnyPassword,
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.BindPassword = "wrong-password" })),
			wantError: `error binding as "cn=admin,dc=pinniped,dc=dev" before user search: LDAP Result Code 49 "Invalid Credentials": `,
		},
		{
			name:                "when the end user password is wrong",
			username:            "pinny",
			password:            "wrong-pinny-password",
			provider:            upstreamldap.New(*providerConfig(nil)),
			wantUnauthenticated: true,
		},
		{
			name:                "when the end user password has the wrong case (passwords are compared as case-sensitive)",
			username:            "pinny",
			password:            strings.ToUpper(pinnyPassword),
			provider:            upstreamldap.New(*providerConfig(nil)),
			wantUnauthenticated: true,
		},
		{
			name:                "when the end user username is wrong",
			username:            "wrong-username",
			password:            pinnyPassword,
			provider:            upstreamldap.New(*providerConfig(nil)),
			wantUnauthenticated: true,
		},
		{
			name:      "when the user search filter does not compile",
			username:  "pinny",
			password:  pinnyPassword,
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.Filter = "*" })),
			wantError: `error searching for user "pinny": LDAP Result Code 201 "Filter Compile Error": ldap: error parsing filter`,
		},
		{
			name:     "when there are too many search results for the user",
			username: "pinny",
			password: pinnyPassword,
			provider: upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) {
				p.UserSearch.Filter = "objectClass=*" // overly broad search filter
			})),
			wantError: `error searching for user "pinny": LDAP Result Code 4 "Size Limit Exceeded": `,
		},
		{
			name:      "when the server is unreachable",
			username:  "pinny",
			password:  pinnyPassword,
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.Host = "127.0.0.1:" + unusedHostPort })),
			wantError: fmt.Sprintf(`error dialing host "127.0.0.1:%s": LDAP Result Code 200 "Network Error": dial tcp 127.0.0.1:%s: connect: connection refused`, unusedHostPort, unusedHostPort),
		},
		{
			name:      "when the server is not parsable",
			username:  "pinny",
			password:  pinnyPassword,
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.Host = "too:many:ports" })),
			wantError: `error dialing host "too:many:ports": LDAP Result Code 200 "Network Error": address too:many:ports: too many colons in address`,
		},
		{
			name:      "when the CA bundle is not parsable",
			username:  "pinny",
			password:  pinnyPassword,
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.CABundle = []byte("invalid-pem") })),
			wantError: fmt.Sprintf(`error dialing host "127.0.0.1:%s": LDAP Result Code 200 "Network Error": could not parse CA bundle`, ldapHostPort),
		},
		{
			name:      "when the CA bundle does not cause the host to be trusted",
			username:  "pinny",
			password:  pinnyPassword,
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.CABundle = nil })),
			wantError: fmt.Sprintf(`error dialing host "127.0.0.1:%s": LDAP Result Code 200 "Network Error": x509: certificate signed by unknown authority`, ldapHostPort),
		},
		{
			name:      "when the UsernameAttribute attribute has multiple values in the entry",
			username:  "wally.ldap@example.com",
			password:  "unused-because-error-is-before-bind",
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.UsernameAttribute = "mail" })),
			wantError: `found 2 values for attribute "mail" while searching for user "wally.ldap@example.com", but expected 1 result`,
		},
		{
			name:      "when the UIDAttribute attribute has multiple values in the entry",
			username:  "wally",
			password:  "unused-because-error-is-before-bind",
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.UIDAttribute = "mail" })),
			wantError: `found 2 values for attribute "mail" while searching for user "wally", but expected 1 result`,
		},
		{
			name:     "when the UsernameAttribute attribute is not found in the entry",
			username: "wally",
			password: "unused-because-error-is-before-bind",
			provider: upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) {
				p.UserSearch.Filter = "cn={}"
				p.UserSearch.UsernameAttribute = "attr-does-not-exist"
			})),
			wantError: `found 0 values for attribute "attr-does-not-exist" while searching for user "wally", but expected 1 result`,
		},
		{
			name:      "when the UIDAttribute attribute is not found in the entry",
			username:  "wally",
			password:  "unused-because-error-is-before-bind",
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.UIDAttribute = "attr-does-not-exist" })),
			wantError: `found 0 values for attribute "attr-does-not-exist" while searching for user "wally", but expected 1 result`,
		},
		{
			name:      "when the UsernameAttribute has the wrong case",
			username:  "Seal",
			password:  pinnyPassword,
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.UsernameAttribute = "SN" })), // this is case-sensitive
			wantError: `found 0 values for attribute "SN" while searching for user "Seal", but expected 1 result`,
		},
		{
			name:      "when the UIDAttribute has the wrong case",
			username:  "pinny",
			password:  pinnyPassword,
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.UIDAttribute = "SN" })), // this is case-sensitive
			wantError: `found 0 values for attribute "SN" while searching for user "pinny", but expected 1 result`,
		},
		{
			name:     "when the UsernameAttribute is DN and has the wrong case",
			username: "pinny",
			password: pinnyPassword,
			provider: upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) {
				p.UserSearch.UsernameAttribute = "DN" // dn must be lower-case
				p.UserSearch.Filter = "cn={}"
			})),
			wantError: `found 0 values for attribute "DN" while searching for user "pinny", but expected 1 result`,
		},
		{
			name:     "when the UIDAttribute is DN and has the wrong case",
			username: "pinny",
			password: pinnyPassword,
			provider: upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) {
				p.UserSearch.UIDAttribute = "DN" // dn must be lower-case
			})),
			wantError: `found 0 values for attribute "DN" while searching for user "pinny", but expected 1 result`,
		},
		{
			name:      "when the search base is invalid",
			username:  "pinny",
			password:  pinnyPassword,
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.Base = "invalid-base" })),
			wantError: `error searching for user "pinny": LDAP Result Code 34 "Invalid DN Syntax": invalid DN`,
		},
		{
			name:      "when the search base does not exist",
			username:  "pinny",
			password:  pinnyPassword,
			provider:  upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.Base = "ou=does-not-exist,dc=pinniped,dc=dev" })),
			wantError: `error searching for user "pinny": LDAP Result Code 32 "No Such Object": `,
		},
		{
			name:                "when the search base causes no search results",
			username:            "pinny",
			password:            pinnyPassword,
			provider:            upstreamldap.New(*providerConfig(func(p *upstreamldap.ProviderConfig) { p.UserSearch.Base = "ou=groups,dc=pinniped,dc=dev" })),
			wantUnauthenticated: true,
		},
		{
			name:                "when there is no username specified",
			username:            "",
			password:            pinnyPassword,
			provider:            upstreamldap.New(*providerConfig(nil)),
			wantUnauthenticated: true,
		},
		{
			name:      "when there is no password specified",
			username:  "pinny",
			password:  "",
			provider:  upstreamldap.New(*providerConfig(nil)),
			wantError: `error binding for user "pinny" using provided password against DN "cn=pinny,ou=users,dc=pinniped,dc=dev": LDAP Result Code 206 "Empty password not allowed by the client": ldap: empty password not allowed by the client`,
		},
		{
			name:                "when the user has no password in their entry",
			username:            "olive",
			password:            "anything",
			provider:            upstreamldap.New(*providerConfig(nil)),
			wantUnauthenticated: true,
		},
	}

	for _, test := range tests {
		tt := test
		t.Run(tt.name, func(t *testing.T) {
			authResponse, authenticated, err := tt.provider.AuthenticateUser(ctx, tt.username, tt.password)

			switch {
			case tt.wantError != "":
				require.EqualError(t, err, tt.wantError)
				require.False(t, authenticated, "expected the user not to be authenticated, but they were")
				require.Nil(t, authResponse)
			case tt.wantUnauthenticated:
				require.NoError(t, err)
				require.False(t, authenticated, "expected the user not to be authenticated, but they were")
				require.Nil(t, authResponse)
			default:
				require.NoError(t, err)
				require.True(t, authenticated, "expected the user to be authenticated, but they were not")
				require.Equal(t, tt.wantAuthResponse, authResponse)
			}
		})
	}
}

func TestSimultaneousRequestsOnSingleProvider(t *testing.T) {
	env := library.IntegrationEnv(t)

	// Note that these tests depend on the values hard-coded in the LDIF file in test/deploy/tools/ldap.yaml.
	// It requires the test LDAP server from the tools deployment.
	if len(env.ToolsNamespace) == 0 {
		t.Skip("Skipping test because it requires the test LDAP server in the tools namespace.")
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancelFunc() // this will send SIGKILL to the subprocess, just in case
	})

	ldapHostPort := findRecentlyUnusedLocalhostPorts(t, 1)[0]

	// Expose the the test LDAP server's TLS port on the localhost.
	startKubectlPortForward(ctx, t, ldapHostPort, "ldaps", "ldap", env.ToolsNamespace)

	provider := upstreamldap.New(*defaultProviderConfig(env, ldapHostPort))

	// Making multiple simultaneous requests on the same upstreamldap.Provider instance should all succeed
	// without triggering the race detector.
	iterations := 150
	resultCh := make(chan authUserResult, iterations)
	for i := 0; i < iterations; i++ {
		go func() {
			authUserCtx, authUserCtxCancelFunc := context.WithTimeout(context.Background(), 2*time.Minute)
			defer authUserCtxCancelFunc()

			authResponse, authenticated, err := provider.AuthenticateUser(authUserCtx,
				env.SupervisorUpstreamLDAP.TestUserCN, env.SupervisorUpstreamLDAP.TestUserPassword,
			)
			resultCh <- authUserResult{
				response:      authResponse,
				authenticated: authenticated,
				err:           err,
			}
		}()
	}
	for i := 0; i < iterations; i++ {
		result := <-resultCh
		// Record failures but allow the test to keep running so that all the background goroutines have a chance to try.
		assert.NoError(t, result.err)
		assert.True(t, result.authenticated, "expected the user to be authenticated, but they were not")
		assert.Equal(t, &authenticator.Response{
			User: &user.DefaultInfo{Name: "pinny", UID: "1000", Groups: []string{}},
		}, result.response)
	}
}

type authUserResult struct {
	response      *authenticator.Response
	authenticated bool
	err           error
}

func defaultProviderConfig(env *library.TestEnv, ldapHostPort string) *upstreamldap.ProviderConfig {
	return &upstreamldap.ProviderConfig{
		Name:         "test-ldap-provider",
		Host:         "127.0.0.1:" + ldapHostPort,
		CABundle:     []byte(env.SupervisorUpstreamLDAP.CABundle),
		BindUsername: "cn=admin,dc=pinniped,dc=dev",
		BindPassword: "password",
		UserSearch: upstreamldap.UserSearchConfig{
			Base:              "ou=users,dc=pinniped,dc=dev",
			Filter:            "", // defaults to UsernameAttribute={}, i.e. "cn={}" in this case
			UsernameAttribute: "cn",
			UIDAttribute:      "uidNumber",
		},
	}
}

func startKubectlPortForward(ctx context.Context, t *testing.T, hostPort, remotePort, serviceName, namespace string) {
	t.Helper()
	startLongRunningCommandAndWaitForInitialOutput(ctx, t,
		"kubectl",
		[]string{
			"port-forward",
			fmt.Sprintf("service/%s", serviceName),
			fmt.Sprintf("%s:%s", hostPort, remotePort),
			"-n", namespace,
		},
		"Forwarding from ",
		"stdout",
	)
}

func findRecentlyUnusedLocalhostPorts(t *testing.T, howManyPorts int) []string {
	t.Helper()

	listeners := []net.Listener{}
	for i := 0; i < howManyPorts; i++ {
		unusedPortGrabbingListener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		listeners = append(listeners, unusedPortGrabbingListener)
	}

	ports := make([]string, len(listeners))
	for i, listener := range listeners {
		splitHostAndPort := strings.Split(listener.Addr().String(), ":")
		require.Len(t, splitHostAndPort, 2)
		ports[i] = splitHostAndPort[1]
	}

	for _, listener := range listeners {
		require.NoError(t, listener.Close())
	}

	return ports
}

func startLongRunningCommandAndWaitForInitialOutput(
	ctx context.Context,
	t *testing.T,
	command string,
	args []string,
	waitForOutputToContain string,
	waitForOutputOnFd string, // can be either "stdout" or "stderr"
) {
	t.Helper()

	t.Logf("Starting: %s %s", command, strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, command, args...)

	var stdoutBuf, stderrBuf syncBuffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	var watchOn *syncBuffer
	switch waitForOutputOnFd {
	case "stdout":
		watchOn = &stdoutBuf
	case "stderr":
		watchOn = &stderrBuf
	default:
		t.Fatalf("oops bad argument")
	}

	err := cmd.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		// If the cancellation of ctx was already scheduled in a t.Cleanup, then this
		// t.Cleanup is registered after the one, so this one will happen first.
		// Cancelling ctx will send SIGKILL, which will act as a backup in case
		// the process ignored this SIGINT.
		err := cmd.Process.Signal(os.Interrupt)
		require.NoError(t, err)
	})

	earlyTerminationCh := make(chan bool, 1)
	go func() {
		err = cmd.Wait()
		earlyTerminationCh <- true
	}()

	terminatedEarly := false
	require.Eventually(t, func() bool {
		t.Logf(`Waiting for %s to emit output: "%s"`, command, waitForOutputToContain)
		if strings.Contains(watchOn.String(), waitForOutputToContain) {
			return true
		}
		select {
		case <-earlyTerminationCh:
			terminatedEarly = true
			return true
		default: // ignore when this non-blocking read found no message
		}
		return false
	}, 1*time.Minute, 1*time.Second)

	require.Falsef(t, terminatedEarly, "subcommand ended sooner than expected")

	t.Logf("Detected that %s has started successfully", command)
}