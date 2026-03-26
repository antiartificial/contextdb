package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Permission represents a single access permission level.
type Permission string

const (
	// PermRead allows read operations (retrieve, get node, walk).
	PermRead Permission = "read"

	// PermWrite allows write operations (write, ingest, label source).
	PermWrite Permission = "write"

	// PermAdmin allows administrative operations (manage tenants, snapshots).
	PermAdmin Permission = "admin"
)

// Token holds parsed authentication information extracted from a request.
type Token struct {
	// Tenant is the tenant identifier from the token.
	Tenant string

	// Permissions is the set of permissions granted by this token.
	Permissions []Permission

	// Raw is the original token string.
	Raw string
}

// HasPermission reports whether the token includes the given permission.
func (t Token) HasPermission(p Permission) bool {
	for _, perm := range t.Permissions {
		if perm == p {
			return true
		}
		// admin implies all permissions
		if perm == PermAdmin {
			return true
		}
	}
	return false
}

// tokenKey is the context key for the parsed auth token.
type tokenKey struct{}

// TokenFromContext extracts the auth Token from the context.
// Returns a zero Token if none is present.
func TokenFromContext(ctx context.Context) Token {
	if v, ok := ctx.Value(tokenKey{}).(Token); ok {
		return v
	}
	return Token{}
}

// withToken returns a context with the auth token set.
func withToken(ctx context.Context, tok Token) context.Context {
	return context.WithValue(ctx, tokenKey{}, tok)
}

// ParseToken parses a raw token string in the format "tenant:perm1,perm2:secret".
// The tenant and secret fields are required; permissions default to ["read"]
// if the permissions segment is empty.
func ParseToken(raw string) (Token, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Token{}, fmt.Errorf("empty token")
	}

	parts := strings.SplitN(raw, ":", 3)
	if len(parts) < 3 {
		return Token{}, fmt.Errorf("invalid token format: expected tenant:permissions:secret")
	}

	tenant := parts[0]
	if tenant == "" {
		return Token{}, fmt.Errorf("invalid token: empty tenant")
	}

	secret := parts[2]
	if secret == "" {
		return Token{}, fmt.Errorf("invalid token: empty secret")
	}

	// Parse permissions
	var perms []Permission
	if parts[1] != "" {
		for _, p := range strings.Split(parts[1], ",") {
			p = strings.TrimSpace(p)
			switch Permission(p) {
			case PermRead, PermWrite, PermAdmin:
				perms = append(perms, Permission(p))
			default:
				return Token{}, fmt.Errorf("unknown permission: %q", p)
			}
		}
	}
	if len(perms) == 0 {
		perms = []Permission{PermRead}
	}

	return Token{
		Tenant:      tenant,
		Permissions: perms,
		Raw:         raw,
	}, nil
}

// AuthMiddleware returns HTTP middleware that extracts and validates a
// token from the Authorization header. The header must use the format:
//
//	Authorization: Bearer tenant:permissions:secret
//
// If no Authorization header is present, the request proceeds without a
// token (anonymous). If the header is present but the token is invalid,
// the request is rejected with 401 Unauthorized.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			next.ServeHTTP(w, r)
			return
		}

		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, `{"error":"invalid authorization scheme, expected Bearer"}`, http.StatusUnauthorized)
			return
		}

		raw := strings.TrimPrefix(auth, "Bearer ")
		tok, err := ParseToken(raw)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid token: %s"}`, err.Error()), http.StatusUnauthorized)
			return
		}

		ctx := withToken(r.Context(), tok)
		// Also propagate tenant for backward compatibility with TenantMiddleware
		ctx = withTenant(ctx, tok.Tenant)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AuthInterceptor returns a gRPC unary server interceptor that extracts
// and validates a token from the "authorization" metadata header.
func AuthInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return handler(ctx, req)
		}

		values := md.Get("authorization")
		if len(values) == 0 {
			return handler(ctx, req)
		}

		auth := values[0]
		if !strings.HasPrefix(auth, "Bearer ") {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization scheme, expected Bearer")
		}

		raw := strings.TrimPrefix(auth, "Bearer ")
		tok, err := ParseToken(raw)
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "invalid token: %s", err.Error())
		}

		ctx = withToken(ctx, tok)
		ctx = withTenant(ctx, tok.Tenant)
		return handler(ctx, req)
	}
}
