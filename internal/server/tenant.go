package server

import (
	"context"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type tenantKey struct{}

// TenantFromContext extracts the tenant ID from context.
func TenantFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(tenantKey{}).(string); ok {
		return v
	}
	return ""
}

// withTenant returns a context with the tenant ID set.
func withTenant(ctx context.Context, tenant string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenant)
}

// TenantInterceptor returns a gRPC unary server interceptor that extracts
// tenant ID from the "x-tenant-id" metadata header.
func TenantInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if ok {
			if values := md.Get("x-tenant-id"); len(values) > 0 {
				ctx = withTenant(ctx, values[0])
			}
		}
		return handler(ctx, req)
	}
}

// TenantMiddleware returns HTTP middleware that extracts tenant ID from
// the "X-Tenant-ID" header or Bearer token prefix.
func TenantMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant := r.Header.Get("X-Tenant-ID")
		if tenant == "" {
			// try Bearer token: "tenant:token"
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				token := strings.TrimPrefix(auth, "Bearer ")
				if parts := strings.SplitN(token, ":", 2); len(parts) == 2 {
					tenant = parts[0]
				}
			}
		}
		if tenant != "" {
			r = r.WithContext(withTenant(r.Context(), tenant))
		}
		next.ServeHTTP(w, r)
	})
}
