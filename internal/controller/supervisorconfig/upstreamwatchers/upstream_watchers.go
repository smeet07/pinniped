// Copyright 2021-2023 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package upstreamwatchers

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"

	"go.pinniped.dev/generated/latest/apis/supervisor/idp/v1alpha1"
	"go.pinniped.dev/internal/constable"
	"go.pinniped.dev/internal/oidc/provider/upstreamprovider"
	"go.pinniped.dev/internal/plog"
	"go.pinniped.dev/internal/upstreamldap"
)

const (
	ReasonNotFound         = "SecretNotFound"
	ReasonWrongType        = "SecretWrongType"
	ReasonMissingKeys      = "SecretMissingKeys"
	ReasonSuccess          = "Success"
	ReasonInvalidTLSConfig = "InvalidTLSConfig"

	ErrNoCertificates = constable.Error("no certificates found")

	LDAPBindAccountSecretType = corev1.SecretTypeBasicAuth
	probeLDAPTimeout          = 90 * time.Second

	// Constants related to conditions.
	typeBindSecretValid              = "BindSecretValid"
	typeTLSConfigurationValid        = "TLSConfigurationValid"
	typeLDAPConnectionValid          = "LDAPConnectionValid"
	TypeSearchBaseFound              = "SearchBaseFound"
	reasonLDAPConnectionError        = "LDAPConnectionError"
	noTLSConfigurationMessage        = "no TLS configuration provided"
	loadedTLSConfigurationMessage    = "loaded TLS configuration"
	ReasonUsingConfigurationFromSpec = "UsingConfigurationFromSpec"
	ReasonErrorFetchingSearchBase    = "ErrorFetchingSearchBase"
)

// ValidatedSettings is the struct which is cached by the ValidatedSettingsCacheI interface.
type ValidatedSettings struct {
	IDPSpecGeneration         int64  // which IDP spec was used during the validation
	BindSecretResourceVersion string // which bind secret was used during the validation

	// Cache the setting for TLS vs StartTLS. This is always auto-discovered by probing the server.
	LDAPConnectionProtocol upstreamldap.LDAPConnectionProtocol

	// Cache the settings for search bases. These could be configured by the IDP spec, or in the
	// case of AD they can also be auto-discovered by probing the server.
	UserSearchBase, GroupSearchBase string

	// Cache copies of the conditions that were computed when the above settings were cached, so we
	// can keep writing them to the status in the future. This matters most when the first attempt
	// to write them to the IDP's status fails. In this case, future Syncs calls will be able to
	// use these cached values to try writing them again.
	ConnectionValidCondition, SearchBaseFoundCondition *v1alpha1.Condition
}

// ValidatedSettingsCacheI is an interface for an in-memory cache with an entry for each upstream
// provider. It keeps track of settings that were already validated for a given IDP spec and bind
// secret for that upstream.
type ValidatedSettingsCacheI interface {
	// Get the cached settings for a given upstream at a given generation which was previously
	// validated using a given bind secret version. If no settings have been cached for the
	// upstream, or if the settings were cached at a different generation of the upstream or
	// using a different version of the bind secret, then return false to indicate that the
	// desired settings were not cached yet for that combination of spec generation and secret version.
	Get(upstreamName, resourceVersion string, idpSpecGeneration int64) (ValidatedSettings, bool)

	// Set some settings into the cache for a given upstream.
	Set(upstreamName string, settings ValidatedSettings)
}

type ValidatedSettingsCache struct {
	ValidatedSettingsByName map[string]ValidatedSettings
}

func NewValidatedSettingsCache() ValidatedSettingsCacheI {
	return &ValidatedSettingsCache{ValidatedSettingsByName: map[string]ValidatedSettings{}}
}

func (s *ValidatedSettingsCache) Get(upstreamName, resourceVersion string, idpSpecGeneration int64) (ValidatedSettings, bool) {
	validatedSettings, found := s.ValidatedSettingsByName[upstreamName]
	if found && validatedSettings.BindSecretResourceVersion == resourceVersion && validatedSettings.IDPSpecGeneration == idpSpecGeneration {
		return validatedSettings, true
	}
	return ValidatedSettings{}, false
}

func (s *ValidatedSettingsCache) Set(upstreamName string, settings ValidatedSettings) {
	s.ValidatedSettingsByName[upstreamName] = settings
}

// UpstreamGenericLDAPIDP is a read-only interface for abstracting the differences between LDAP and Active Directory IDP types.
type UpstreamGenericLDAPIDP interface {
	Spec() UpstreamGenericLDAPSpec
	Name() string
	Namespace() string
	Generation() int64
	Status() UpstreamGenericLDAPStatus
}

type UpstreamGenericLDAPSpec interface {
	Host() string
	TLSSpec() *v1alpha1.TLSSpec
	BindSecretName() string
	UserSearch() UpstreamGenericLDAPUserSearch
	GroupSearch() UpstreamGenericLDAPGroupSearch
	DetectAndSetSearchBase(ctx context.Context, config *upstreamldap.ProviderConfig) *v1alpha1.Condition
}

type UpstreamGenericLDAPUserSearch interface {
	Base() string
	Filter() string
	UsernameAttribute() string
	UIDAttribute() string
}

type UpstreamGenericLDAPGroupSearch interface {
	Base() string
	Filter() string
	GroupNameAttribute() string
}

type UpstreamGenericLDAPStatus interface {
	Conditions() []v1alpha1.Condition
}

func ValidateTLSConfig(tlsSpec *v1alpha1.TLSSpec, config *upstreamldap.ProviderConfig) *v1alpha1.Condition {
	if tlsSpec == nil {
		return validTLSCondition(noTLSConfigurationMessage)
	}
	if len(tlsSpec.CertificateAuthorityData) == 0 {
		return validTLSCondition(loadedTLSConfigurationMessage)
	}

	bundle, err := base64.StdEncoding.DecodeString(tlsSpec.CertificateAuthorityData)
	if err != nil {
		return invalidTLSCondition(fmt.Sprintf("certificateAuthorityData is invalid: %s", err.Error()))
	}

	ca := x509.NewCertPool()
	ok := ca.AppendCertsFromPEM(bundle)
	if !ok {
		return invalidTLSCondition(fmt.Sprintf("certificateAuthorityData is invalid: %s", ErrNoCertificates))
	}

	config.CABundle = bundle
	return validTLSCondition(loadedTLSConfigurationMessage)
}

func TestConnection(
	ctx context.Context,
	bindSecretName string,
	config *upstreamldap.ProviderConfig,
	currentSecretVersion string,
) *v1alpha1.Condition {
	// First try using TLS.
	config.ConnectionProtocol = upstreamldap.TLS
	tlsLDAPProvider := upstreamldap.New(*config)
	err := tlsLDAPProvider.TestConnection(ctx)
	if err != nil {
		plog.InfoErr("testing LDAP connection using TLS failed, so trying again with StartTLS", err, "host", config.Host)
		// If there was any error, try again with StartTLS instead.
		config.ConnectionProtocol = upstreamldap.StartTLS
		startTLSLDAPProvider := upstreamldap.New(*config)
		startTLSErr := startTLSLDAPProvider.TestConnection(ctx)
		if startTLSErr == nil {
			plog.Info("testing LDAP connection using StartTLS succeeded", "host", config.Host)
			// Successfully able to fall back to using StartTLS, so clear the original
			// error and consider the connection test to be successful.
			err = nil
		} else {
			plog.InfoErr("testing LDAP connection using StartTLS also failed", err, "host", config.Host)
			// Falling back to StartTLS also failed, so put TLS back into the config
			// and consider the connection test to be failed.
			config.ConnectionProtocol = upstreamldap.TLS
		}
	}

	if err != nil {
		return &v1alpha1.Condition{
			Type:   typeLDAPConnectionValid,
			Status: v1alpha1.ConditionFalse,
			Reason: reasonLDAPConnectionError,
			Message: fmt.Sprintf(`could not successfully connect to "%s" and bind as user "%s": %s`,
				config.Host, config.BindUsername, err.Error()),
		}
	}

	return &v1alpha1.Condition{
		Type:   typeLDAPConnectionValid,
		Status: v1alpha1.ConditionTrue,
		Reason: ReasonSuccess,
		Message: fmt.Sprintf(`successfully able to connect to "%s" and bind as user "%s" [validated with Secret "%s" at version "%s"]`,
			config.Host, config.BindUsername, bindSecretName, currentSecretVersion),
	}
}

func validTLSCondition(message string) *v1alpha1.Condition {
	return &v1alpha1.Condition{
		Type:    typeTLSConfigurationValid,
		Status:  v1alpha1.ConditionTrue,
		Reason:  ReasonSuccess,
		Message: message,
	}
}

func invalidTLSCondition(message string) *v1alpha1.Condition {
	return &v1alpha1.Condition{
		Type:    typeTLSConfigurationValid,
		Status:  v1alpha1.ConditionFalse,
		Reason:  ReasonInvalidTLSConfig,
		Message: message,
	}
}

func ValidateSecret(secretInformer corev1informers.SecretInformer, secretName string, secretNamespace string, config *upstreamldap.ProviderConfig) (*v1alpha1.Condition, string) {
	secret, err := secretInformer.Lister().Secrets(secretNamespace).Get(secretName)
	if err != nil {
		return &v1alpha1.Condition{
			Type:    typeBindSecretValid,
			Status:  v1alpha1.ConditionFalse,
			Reason:  ReasonNotFound,
			Message: err.Error(),
		}, ""
	}

	if secret.Type != corev1.SecretTypeBasicAuth {
		return &v1alpha1.Condition{
			Type:   typeBindSecretValid,
			Status: v1alpha1.ConditionFalse,
			Reason: ReasonWrongType,
			Message: fmt.Sprintf("referenced Secret %q has wrong type %q (should be %q)",
				secretName, secret.Type, corev1.SecretTypeBasicAuth),
		}, secret.ResourceVersion
	}

	config.BindUsername = string(secret.Data[corev1.BasicAuthUsernameKey])
	config.BindPassword = string(secret.Data[corev1.BasicAuthPasswordKey])
	if len(config.BindUsername) == 0 || len(config.BindPassword) == 0 {
		return &v1alpha1.Condition{
			Type:   typeBindSecretValid,
			Status: v1alpha1.ConditionFalse,
			Reason: ReasonMissingKeys,
			Message: fmt.Sprintf("referenced Secret %q is missing required keys %q",
				secretName, []string{corev1.BasicAuthUsernameKey, corev1.BasicAuthPasswordKey}),
		}, secret.ResourceVersion
	}

	return &v1alpha1.Condition{
		Type:    typeBindSecretValid,
		Status:  v1alpha1.ConditionTrue,
		Reason:  ReasonSuccess,
		Message: "loaded bind secret",
	}, secret.ResourceVersion
}

// gradatedCondition is a condition and a boolean that tells you whether the condition is fatal or just a warning.
type gradatedCondition struct {
	condition *v1alpha1.Condition
	isFatal   bool
}

// GradatedConditions is a list of conditions, where each condition can additionally be considered fatal or non-fatal.
type GradatedConditions struct {
	gradatedConditions []gradatedCondition
}

func (g *GradatedConditions) Conditions() []*v1alpha1.Condition {
	conditions := []*v1alpha1.Condition{}
	for _, gc := range g.gradatedConditions {
		conditions = append(conditions, gc.condition)
	}
	return conditions
}

func (g *GradatedConditions) Append(condition *v1alpha1.Condition, isFatal bool) {
	g.gradatedConditions = append(g.gradatedConditions, gradatedCondition{condition: condition, isFatal: isFatal})
}

func ValidateGenericLDAP(
	ctx context.Context,
	upstream UpstreamGenericLDAPIDP,
	secretInformer corev1informers.SecretInformer,
	validatedSettingsCache ValidatedSettingsCacheI,
	config *upstreamldap.ProviderConfig,
) GradatedConditions {
	conditions := GradatedConditions{}

	secretValidCondition, currentSecretVersion := ValidateSecret(secretInformer, upstream.Spec().BindSecretName(), upstream.Namespace(), config)
	conditions.Append(secretValidCondition, true)

	tlsValidCondition := ValidateTLSConfig(upstream.Spec().TLSSpec(), config)
	conditions.Append(tlsValidCondition, true)

	var ldapConnectionValidCondition, searchBaseFoundCondition *v1alpha1.Condition
	// No point in trying to connect to the server if the config was already determined to be invalid.
	if secretValidCondition.Status == v1alpha1.ConditionTrue && tlsValidCondition.Status == v1alpha1.ConditionTrue {
		ldapConnectionValidCondition, searchBaseFoundCondition = validateAndSetLDAPServerConnectivityAndSearchBase(ctx, validatedSettingsCache, upstream, config, currentSecretVersion)
		conditions.Append(ldapConnectionValidCondition, false)
		if searchBaseFoundCondition != nil { // currently, only used for AD, so may be nil
			conditions.Append(searchBaseFoundCondition, true)
		}
	}
	return conditions
}

func validateAndSetLDAPServerConnectivityAndSearchBase(
	ctx context.Context,
	validatedSettingsCache ValidatedSettingsCacheI,
	upstream UpstreamGenericLDAPIDP,
	config *upstreamldap.ProviderConfig,
	currentSecretVersion string,
) (*v1alpha1.Condition, *v1alpha1.Condition) {
	validatedSettings, hasPreviousValidatedSettings := validatedSettingsCache.Get(upstream.Name(), currentSecretVersion, upstream.Generation())
	var ldapConnectionValidCondition, searchBaseFoundCondition *v1alpha1.Condition

	if hasPreviousValidatedSettings && validatedSettings.UserSearchBase != "" && validatedSettings.GroupSearchBase != "" {
		// Found previously validated settings in the cache (which is also not missing search base fields), so use them.
		config.ConnectionProtocol = validatedSettings.LDAPConnectionProtocol
		config.UserSearch.Base = validatedSettings.UserSearchBase
		config.GroupSearch.Base = validatedSettings.GroupSearchBase
		ldapConnectionValidCondition = validatedSettings.ConnectionValidCondition.DeepCopy()
		searchBaseFoundCondition = validatedSettings.SearchBaseFoundCondition.DeepCopy()
	} else {
		// Did not find previously validated settings in the cache, so probe the LDAP server.
		testConnectionTimeout, cancelFunc := context.WithTimeout(ctx, probeLDAPTimeout)
		defer cancelFunc()
		ldapConnectionValidCondition = TestConnection(testConnectionTimeout, upstream.Spec().BindSecretName(), config, currentSecretVersion)

		searchBaseTimeout, cancelFunc := context.WithTimeout(ctx, probeLDAPTimeout)
		defer cancelFunc()
		searchBaseFoundCondition = upstream.Spec().DetectAndSetSearchBase(searchBaseTimeout, config)

		// When there were no failures, write the newly validated settings to the cache.
		// It's okay for the search base condition to be nil, since it's only used by Active Directory providers,
		// but if it exists make sure it was not a failure.
		if ldapConnectionValidCondition.Status == v1alpha1.ConditionTrue &&
			(searchBaseFoundCondition == nil || (searchBaseFoundCondition.Status == v1alpha1.ConditionTrue)) {
			// Remember (in-memory for this pod) that the controller has successfully validated the LDAP or AD provider
			// using this version of the Secret. This is for performance reasons, to avoid attempting to connect to
			// the LDAP server more than is needed. If the pod restarts, it will attempt this validation again.
			validatedSettingsCache.Set(upstream.Name(), ValidatedSettings{
				IDPSpecGeneration:         upstream.Generation(),
				BindSecretResourceVersion: currentSecretVersion,
				LDAPConnectionProtocol:    config.ConnectionProtocol,
				UserSearchBase:            config.UserSearch.Base,
				GroupSearchBase:           config.GroupSearch.Base,
				ConnectionValidCondition:  ldapConnectionValidCondition.DeepCopy(),
				SearchBaseFoundCondition:  searchBaseFoundCondition.DeepCopy(), // currently, only used for AD, so may be nil
			})
		}
	}

	return ldapConnectionValidCondition, searchBaseFoundCondition
}

func EvaluateConditions(conditions GradatedConditions, config *upstreamldap.ProviderConfig) (upstreamprovider.UpstreamLDAPIdentityProviderI, bool) {
	for _, gradatedCondition := range conditions.gradatedConditions {
		if gradatedCondition.condition.Status != v1alpha1.ConditionTrue && gradatedCondition.isFatal {
			// Invalid provider, so do not load it into the cache.
			return nil, true
		}
	}

	for _, gradatedCondition := range conditions.gradatedConditions {
		if gradatedCondition.condition.Status != v1alpha1.ConditionTrue && !gradatedCondition.isFatal {
			// Error but load it into the cache anyway, treating this condition failure more like a warning.
			// Try again hoping that the condition will improve.
			return upstreamldap.New(*config), true
		}
	}
	// Fully validated provider, so load it into the cache.
	return upstreamldap.New(*config), false
}
