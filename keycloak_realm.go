package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// keycloakRealmField maps one top-level keycloak_realm scalar/list
// argument to its identically-spelled Keycloak RealmRepresentation
// field — taken directly from each option's own documented `aliases`
// entry in `ansible-doc community.general.keycloak_realm` (the
// camelCase alias IS the real API field name, per this module's own
// doc: "Aliases are provided so camelCased versions can be used as
// well"), not guessed. kind is one of "bool", "int", "string", "list".
//
// Deviation from real keycloak_realm: the ~20
// `web_authn_policy_*`/`web_authn_policy_passwordless_*` fields (a
// deeply nested WebAuthn policy sub-object of RealmRepresentation) are
// NOT included here — an honestly-documented gap for a niche,
// rarely-templated settings group, rather than 20 more mechanical table
// rows this port could not verify field-by-field without a live
// server's own WebAuthn-enabled realm to diff against.
var keycloakRealmFields = []struct {
	arg, api, kind string
}{
	{"access_code_lifespan", "accessCodeLifespan", "int"},
	{"access_code_lifespan_login", "accessCodeLifespanLogin", "int"},
	{"access_code_lifespan_user_action", "accessCodeLifespanUserAction", "int"},
	{"access_token_lifespan", "accessTokenLifespan", "int"},
	{"access_token_lifespan_for_implicit_flow", "accessTokenLifespanForImplicitFlow", "int"},
	{"account_theme", "accountTheme", "string"},
	{"action_token_generated_by_admin_lifespan", "actionTokenGeneratedByAdminLifespan", "int"},
	{"action_token_generated_by_user_lifespan", "actionTokenGeneratedByUserLifespan", "int"},
	{"admin_events_details_enabled", "adminEventsDetailsEnabled", "bool"},
	{"admin_events_enabled", "adminEventsEnabled", "bool"},
	{"admin_permissions_enabled", "adminPermissionsEnabled", "bool"},
	{"admin_theme", "adminTheme", "string"},
	{"browser_flow", "browserFlow", "string"},
	{"brute_force_protected", "bruteForceProtected", "bool"},
	{"brute_force_strategy", "bruteForceStrategy", "string"},
	{"client_authentication_flow", "clientAuthenticationFlow", "string"},
	{"client_offline_session_idle_timeout", "clientOfflineSessionIdleTimeout", "int"},
	{"client_offline_session_max_lifespan", "clientOfflineSessionMaxLifespan", "int"},
	{"client_session_idle_timeout", "clientSessionIdleTimeout", "int"},
	{"client_session_max_lifespan", "clientSessionMaxLifespan", "int"},
	{"default_default_client_scopes", "defaultDefaultClientScopes", "list"},
	{"default_groups", "defaultGroups", "list"},
	{"default_locale", "defaultLocale", "string"},
	{"default_optional_client_scopes", "defaultOptionalClientScopes", "list"},
	{"default_roles", "defaultRoles", "list"},
	{"default_signature_algorithm", "defaultSignatureAlgorithm", "string"},
	{"direct_grant_flow", "directGrantFlow", "string"},
	{"display_name", "displayName", "string"},
	{"display_name_html", "displayNameHtml", "string"},
	{"docker_authentication_flow", "dockerAuthenticationFlow", "string"},
	{"duplicate_emails_allowed", "duplicateEmailsAllowed", "bool"},
	{"edit_username_allowed", "editUsernameAllowed", "bool"},
	{"email_theme", "emailTheme", "string"},
	{"enabled", "enabled", "bool"},
	{"enabled_event_types", "enabledEventTypes", "list"},
	{"events_enabled", "eventsEnabled", "bool"},
	{"events_expiration", "eventsExpiration", "int"},
	{"events_listeners", "eventsListeners", "list"},
	{"failure_factor", "failureFactor", "int"},
	{"first_broker_login_flow", "firstBrokerLoginFlow", "string"},
	{"internationalization_enabled", "internationalizationEnabled", "bool"},
	{"login_theme", "loginTheme", "string"},
	{"login_with_email_allowed", "loginWithEmailAllowed", "bool"},
	{"max_delta_time_seconds", "maxDeltaTimeSeconds", "int"},
	{"max_failure_wait_seconds", "maxFailureWaitSeconds", "int"},
	{"max_secondary_auth_failures", "maxSecondaryAuthFailures", "int"},
	{"max_temporary_lockouts", "maxTemporaryLockouts", "int"},
	{"minimum_quick_login_wait_seconds", "minimumQuickLoginWaitSeconds", "int"},
	{"not_before", "notBefore", "int"},
	{"oauth2_device_code_lifespan", "oauth2DeviceCodeLifespan", "int"},
	{"oauth2_device_polling_interval", "oauth2DevicePollingInterval", "int"},
	{"offline_session_idle_timeout", "offlineSessionIdleTimeout", "int"},
	{"offline_session_max_lifespan", "offlineSessionMaxLifespan", "int"},
	{"offline_session_max_lifespan_enabled", "offlineSessionMaxLifespanEnabled", "bool"},
	{"organizations_enabled", "organizationsEnabled", "bool"},
	{"otp_policy_algorithm", "otpPolicyAlgorithm", "string"},
	{"otp_policy_digits", "otpPolicyDigits", "int"},
	{"otp_policy_initial_counter", "otpPolicyInitialCounter", "int"},
	{"otp_policy_look_ahead_window", "otpPolicyLookAheadWindow", "int"},
	{"otp_policy_period", "otpPolicyPeriod", "int"},
	{"otp_policy_type", "otpPolicyType", "string"},
	{"otp_supported_applications", "otpSupportedApplications", "list"},
	{"password_policy", "passwordPolicy", "string"},
	{"permanent_lockout", "permanentLockout", "bool"},
	{"quick_login_check_milli_seconds", "quickLoginCheckMilliSeconds", "int"},
	{"refresh_token_max_reuse", "refreshTokenMaxReuse", "int"},
	{"registration_allowed", "registrationAllowed", "bool"},
	{"registration_email_as_username", "registrationEmailAsUsername", "bool"},
	{"registration_flow", "registrationFlow", "string"},
	{"remember_me", "rememberMe", "bool"},
	{"reset_credentials_flow", "resetCredentialsFlow", "string"},
	{"reset_password_allowed", "resetPasswordAllowed", "bool"},
	{"revoke_refresh_token", "revokeRefreshToken", "bool"},
	{"ssl_required", "sslRequired", "string"},
	{"sso_session_idle_timeout", "ssoSessionIdleTimeout", "int"},
	{"sso_session_idle_timeout_remember_me", "ssoSessionIdleTimeoutRememberMe", "int"},
	{"sso_session_max_lifespan", "ssoSessionMaxLifespan", "int"},
	{"sso_session_max_lifespan_remember_me", "ssoSessionMaxLifespanRememberMe", "int"},
	{"supported_locales", "supportedLocales", "list"},
	{"user_managed_access_allowed", "userManagedAccessAllowed", "bool"},
	{"verify_email", "verifyEmail", "bool"},
	{"wait_increment_seconds", "waitIncrementSeconds", "int"},
}

// keycloakRealmDictFields maps the remaining keycloak_realm dict-typed
// arguments straight through to their identically-shaped API field,
// unconditionally when given, on both create and update (never diffed
// field-by-field, matching this port's own gitlab_project.go
// container_expiration_policy handling for the same reason: a nested
// object cheaper to always send than to shallow-diff).
var keycloakRealmDictFields = []struct{ arg, api string }{
	{"attributes", "attributes"},
	{"browser_security_headers", "browserSecurityHeaders"},
	{"client_scope_mappings", "clientScopeMappings"},
	{"localization_texts", "localizationTexts"},
	{"smtp_server", "smtpServer"},
}

// moduleKeycloakRealm implements Ansible's `keycloak_realm`
// (community.general) module: creates, updates, or deletes a Keycloak
// realm, via `kcadm.sh create/get/update/delete realms(/<realm>)` —
// realm objects live directly under `admin/realms`, not
// `admin/realms/{realm}/...` the way every other resource in this
// batch does, so this module never passes `-r` to kcadm at all — see
// keycloak_common.go's own doc comment for the kcadm.sh substitution.
//
// Args: realm (required — the realm name, used as both the create
// body's own `realm` field and the `realms/<realm>` path for
// get/update/delete); id (optional — sets the representation's own
// `id` field, which Keycloak defaults equal to `realm` when omitted);
// state (present|absent, default present); every keycloakRealmFields
// and keycloakRealmDictFields entry above.
func moduleKeycloakRealm(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_realm"); !ok {
		return res, nil
	}
	realm, err := requireString(args, "realm")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("keycloak_realm: state must be one of present, absent, got %q", state)
	}
	path := "realms/" + realm

	current, present, err := kcadmShow(ctx, conn, path, "")
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok("Realm " + realm + " does not exist"), nil
		}
		res, err := kcadmDelete(ctx, conn, path, "")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_realm", "unable to delete realm "+realm, res), nil
		}
		return Changed("Realm " + realm + " has been deleted"), nil
	}

	if !present {
		body := map[string]any{"realm": realm}
		if id := argString(args, "id", ""); id != "" {
			body["id"] = id
		}
		for _, f := range keycloakRealmFields {
			if v, ok := args[f.arg]; ok {
				body[f.api] = keycloakRealmValue(f.kind, v)
			}
		}
		for _, f := range keycloakRealmDictFields {
			if v, ok := args[f.arg]; ok {
				body[f.api] = v
			}
		}
		res, err := kcadmCreateBody(ctx, conn, "realms", "", body)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf("keycloak_realm", "unable to create realm "+realm, res), nil
		}
		return Changed("Realm " + realm + " has been created"), nil
	}

	body := map[string]any{}
	for k, v := range current {
		body[k] = v
	}
	changed := false
	for _, f := range keycloakRealmFields {
		if v, ok := args[f.arg]; ok {
			want := keycloakRealmValue(f.kind, v)
			if have, existed := current[f.api]; !existed || !jsonScalarEqual(want, have) {
				body[f.api] = want
				changed = true
			}
		}
	}
	for _, f := range keycloakRealmDictFields {
		if v, ok := args[f.arg]; ok {
			body[f.api] = v
			changed = true
		}
	}
	if !changed {
		return Ok("Realm " + realm + " already up to date"), nil
	}
	res, err := kcadmUpdateBody(ctx, conn, path, "", body)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_realm", "unable to update realm "+realm, res), nil
	}
	return Changed("Realm " + realm + " has been updated"), nil
}

// keycloakRealmValue coerces v per kind ("bool"|"int"|"list"|"string").
func keycloakRealmValue(kind string, v any) any {
	switch kind {
	case "bool":
		if b, ok := v.(bool); ok {
			return b
		}
		return argBool(map[string]any{"v": v}, "v", false)
	case "int":
		return argInt(map[string]any{"v": v}, "v", 0)
	case "list":
		return argStringList(map[string]any{"v": v}, "v")
	default:
		return argString(map[string]any{"v": v}, "v", "")
	}
}
