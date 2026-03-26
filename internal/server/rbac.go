package server

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CanRead reports whether the token grants read access to the given
// tenant and namespace. A zero token (anonymous) is granted read access
// to allow backward compatibility with unauthenticated deployments.
func CanRead(token Token, tenant, namespace string) bool {
	// Anonymous access is permitted for reads
	if token.Raw == "" {
		return true
	}
	// Token tenant must match the requested tenant
	if token.Tenant != tenant && tenant != "" {
		return false
	}
	return token.HasPermission(PermRead)
}

// CanWrite reports whether the token grants write access to the given
// tenant and namespace.
func CanWrite(token Token, tenant, namespace string) bool {
	if token.Raw == "" {
		// Anonymous writes are allowed when no auth is configured
		return true
	}
	if token.Tenant != tenant && tenant != "" {
		return false
	}
	return token.HasPermission(PermWrite)
}

// IsAdmin reports whether the token grants admin access for the given tenant.
func IsAdmin(token Token, tenant string) bool {
	if token.Raw == "" {
		return false
	}
	if token.Tenant != tenant && tenant != "" {
		return false
	}
	return token.HasPermission(PermAdmin)
}

// RequirePermission checks the context token for the given permission.
// Returns nil if the permission is present, or an appropriate error if not.
// For gRPC contexts, returns a codes.PermissionDenied status error.
// For other contexts, returns a plain error.
func RequirePermission(ctx context.Context, perm Permission) error {
	tok := TokenFromContext(ctx)

	// If no token is present, allow for backward compatibility
	// (unauthenticated deployments). Admin operations always require a token.
	if tok.Raw == "" {
		if perm == PermAdmin {
			return permissionError(perm)
		}
		return nil
	}

	if !tok.HasPermission(perm) {
		return permissionError(perm)
	}
	return nil
}

func permissionError(perm Permission) error {
	msg := fmt.Sprintf("permission denied: requires %q", string(perm))
	// Return gRPC-compatible status error that also works as a plain error
	return status.Error(codes.PermissionDenied, msg)
}
