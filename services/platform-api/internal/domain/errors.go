package domain

import (
	"fmt"
	"net/http"
)

type AppError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

func (e *AppError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func NewAppError(code, message string, httpStatus int) *AppError {
	return &AppError{Code: code, Message: message, HTTPStatus: httpStatus}
}

var (
	ErrUnauthorized          = NewAppError("unauthorized", "authentication required", http.StatusUnauthorized)
	ErrForbidden             = NewAppError("forbidden", "insufficient permissions", http.StatusForbidden)
	ErrTenantNotFound        = NewAppError("tenant_not_found", "tenant not found", http.StatusNotFound)
	ErrUserNotFound          = NewAppError("user_not_found", "user not found", http.StatusNotFound)
	ErrDeviceNotFound        = NewAppError("device_not_found", "device not found", http.StatusNotFound)
	ErrSessionNotFound       = NewAppError("session_not_found", "session not found", http.StatusNotFound)
	ErrApprovalNotFound      = NewAppError("approval_not_found", "approval request not found", http.StatusNotFound)
	ErrApprovalExpired       = NewAppError("approval_expired", "approval request has expired", http.StatusConflict)
	ErrApprovalInvalidState  = NewAppError("approval_invalid_state", "approval request is in an invalid state for this operation", http.StatusConflict)
	ErrPolicyViolation       = NewAppError("policy_violation", "action violates security policy", http.StatusForbidden)
	ErrDeviceRevoked         = NewAppError("device_revoked", "device has been revoked", http.StatusForbidden)
	ErrInvalidEnrollToken    = NewAppError("invalid_enrollment_token", "enrollment token is invalid or expired", http.StatusBadRequest)
	ErrConfigVersionConflict = NewAppError("config_version_conflict", "configuration version conflict", http.StatusConflict)
	ErrRateLimited           = NewAppError("rate_limited", "too many requests", http.StatusTooManyRequests)
	ErrInternalError         = NewAppError("internal_error", "internal server error", http.StatusInternalServerError)
	ErrInvalidCredentials    = NewAppError("invalid_credentials", "invalid email or password", http.StatusUnauthorized)
	ErrProfileNotFound       = NewAppError("profile_not_found", "profile not found", http.StatusNotFound)
	ErrPackageNotFound       = NewAppError("package_not_found", "download package not found", http.StatusNotFound)
	ErrDuplicateDownloadPackage = NewAppError("duplicate_download_package", "A download package already exists for this agent profile with the same distribution mode, platform, architecture, version, and package type. Delete the existing package or change one of these fields.", http.StatusConflict)
	ErrTokenNotFound         = NewAppError("token_not_found", "enrollment token not found", http.StatusNotFound)
	ErrDuplicateSlug         = NewAppError("duplicate_slug", "slug already exists", http.StatusConflict)
	ErrDuplicateName         = NewAppError("duplicate_name", "name already exists for this tenant", http.StatusConflict)
	ErrSessionInvalidState   = NewAppError("session_invalid_state", "session is in an invalid state for this operation", http.StatusConflict)
	ErrInvalidRequest        = NewAppError("invalid_request", "invalid request parameters", http.StatusBadRequest)
	ErrNotFound              = NewAppError("not_found", "resource not found", http.StatusNotFound)
	ErrActivationKeyInvalid  = NewAppError("activation_key_invalid", "activation key is invalid", http.StatusUnauthorized)
	ErrDeviceLimitReached    = NewAppError("device_limit_reached", "maximum device binding limit reached", http.StatusConflict)
	ErrDeviceAlreadyBound    = NewAppError("device_already_bound", "device is already bound to this package", http.StatusConflict)
	ErrDeviceCodeNotFound    = NewAppError("device_code_not_found", "device code not found in pending list", http.StatusNotFound)
	ErrBindingNotFound       = NewAppError("binding_not_found", "device binding not found", http.StatusNotFound)
	ErrBindingRevoked        = NewAppError("binding_revoked", "device binding has been revoked", http.StatusForbidden)
	ErrFingerprintMismatch   = NewAppError("fingerprint_mismatch", "hardware fingerprint does not match", http.StatusForbidden)
	ErrIncorrectPassword     = NewAppError("incorrect_password", "current password is incorrect", http.StatusBadRequest)

	ErrCommandTaskNotFound         = NewAppError("command_task_not_found", "command task not found", http.StatusNotFound)
	ErrCommandTaskInvalidState     = NewAppError("command_task_invalid_state", "command task is in an invalid state for this operation", http.StatusConflict)
	ErrCommandTaskExpired          = NewAppError("command_task_expired", "command task has expired", http.StatusConflict)
	ErrApprovalPolicyNotFound      = NewAppError("approval_policy_not_found", "approval policy not found", http.StatusNotFound)
	ErrIMProviderNotFound          = NewAppError("im_provider_not_found", "IM provider not found", http.StatusNotFound)
	ErrNotificationChannelNotFound = NewAppError("notification_channel_not_found", "notification channel not found", http.StatusNotFound)
	ErrInsufficientPermission      = NewAppError("insufficient_permission", "you do not have permission for this operation", http.StatusForbidden)
	ErrSeparationOfDutyViolation   = NewAppError("sod_violation", "creator cannot approve their own task", http.StatusForbidden)

	ErrDeviceAuthNotFound      = NewAppError("device_auth_not_found", "device authorization session not found", http.StatusNotFound)
	ErrDeviceAuthExpired       = NewAppError("device_auth_expired", "device authorization session has expired", http.StatusBadRequest)
	ErrDeviceAuthDenied        = NewAppError("device_auth_denied", "user denied the authorization request", http.StatusForbidden)
	ErrDeviceAuthInvalidState  = NewAppError("device_auth_invalid_state", "device authorization session is in an invalid state for this operation", http.StatusConflict)
	ErrIdeClientTokenNotFound  = NewAppError("ide_client_token_not_found", "IDE client token not found or invalid", http.StatusUnauthorized)
	ErrIdeClientTokenExpired   = NewAppError("ide_client_token_expired", "IDE client refresh token has expired", http.StatusUnauthorized)

	ErrApiTokenNotFound  = NewAppError("api_token_not_found", "API token not found or invalid", http.StatusUnauthorized)
	ErrApiTokenRevoked   = NewAppError("api_token_revoked", "API token has been revoked", http.StatusUnauthorized)
	ErrApiTokenExpired   = NewAppError("api_token_expired", "API token has expired", http.StatusUnauthorized)

	ErrMarketplaceItemNotFound       = NewAppError("marketplace_item_not_found", "marketplace item not found", http.StatusNotFound)
	ErrMarketplaceItemNotPublished   = NewAppError("marketplace_item_not_published", "marketplace item is not published", http.StatusBadRequest)
	ErrMarketplaceAlreadySubscribed  = NewAppError("marketplace_already_subscribed", "tenant is already subscribed to this item", http.StatusConflict)
	ErrMarketplaceSubscriptionNotFound = NewAppError("marketplace_subscription_not_found", "subscription not found", http.StatusNotFound)
	ErrObjectStorageUnavailable        = NewAppError("object_storage_unavailable", "object storage is not configured or unavailable", http.StatusServiceUnavailable)
)
