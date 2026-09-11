package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TokenRegistry is opt-in authentication. Tokens are exact configured values,
// never merely syntactically valid bearer strings.
type TokenRegistry struct{ tokens []Token }

func NewTokenRegistry(raw string) (*TokenRegistry, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("auth tokens must be a JSON array: %w", err)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("auth token registry is empty")
	}
	r := &TokenRegistry{}
	for _, v := range values {
		t, err := ParseToken(v)
		if err != nil {
			return nil, fmt.Errorf("invalid configured token: %w", err)
		}
		r.tokens = append(r.tokens, t)
	}
	return r, nil
}

func (r *TokenRegistry) GRPCInterceptor() grpc.UnaryServerInterceptor {
	if r == nil {
		return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			return h(ctx, req)
		}
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		v := md.Get("authorization")
		if len(v) == 0 || !strings.HasPrefix(v[0], "Bearer ") {
			return nil, status.Error(codes.Unauthenticated, "unauthorized")
		}
		tok, ok := r.authenticate(strings.TrimPrefix(v[0], "Bearer "))
		if !ok {
			return nil, status.Error(codes.PermissionDenied, "forbidden")
		}
		if tenant := md.Get("x-tenant-id"); len(tenant) > 0 && tenant[0] != tok.Tenant {
			return nil, status.Error(codes.PermissionDenied, "tenant mismatch")
		}
		needed := grpcPermission(info.FullMethod)
		if !tok.HasPermission(needed) {
			return nil, status.Error(codes.PermissionDenied, "forbidden")
		}
		return h(withTenant(withToken(ctx, tok), tok.Tenant), req)
	}
}
func grpcPermission(method string) Permission {
	name := method[strings.LastIndex(method, "/")+1:]
	switch name {
	case "Retrieve", "Ping", "StreamRetrieve":
		return PermRead
	case "Write", "IngestText", "LabelSource", "ValidateClaim", "RefuteClaim", "MarkUseful", "MarkStale":
		return PermWrite
	default:
		return PermAdmin
	}
}
func (r *TokenRegistry) GRPCStreamInterceptor() grpc.StreamServerInterceptor {
	if r == nil {
		return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
			return h(srv, ss)
		}
	}
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		md, _ := metadata.FromIncomingContext(ss.Context())
		v := md.Get("authorization")
		if len(v) == 0 || !strings.HasPrefix(v[0], "Bearer ") {
			return status.Error(codes.Unauthenticated, "unauthorized")
		}
		tok, ok := r.authenticate(strings.TrimPrefix(v[0], "Bearer "))
		if !ok || !tok.HasPermission(grpcPermission(info.FullMethod)) {
			return status.Error(codes.PermissionDenied, "forbidden")
		}
		return h(srv, &authStream{ServerStream: ss, ctx: withTenant(withToken(ss.Context(), tok), tok.Tenant)})
	}
}

type authStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authStream) Context() context.Context { return s.ctx }
func (r *TokenRegistry) authenticate(raw string) (Token, bool) {
	for _, t := range r.tokens {
		if len(raw) == len(t.Raw) && subtle.ConstantTimeCompare([]byte(raw), []byte(t.Raw)) == 1 {
			return t, true
		}
	}
	return Token{}, false
}
func requiredPermission(r *http.Request) Permission {
	if r.URL.Path == "/admin/" || strings.HasPrefix(r.URL.Path, "/admin/") {
		return PermAdmin
	}
	if r.URL.Path == "/graphql" {
		return PermAdmin
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return PermRead
	}
	return PermWrite
}
func (r *TokenRegistry) Middleware(next http.Handler) http.Handler {
	if r == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if q.URL.Path == "/health" {
			next.ServeHTTP(w, q)
			return
		}
		auth := q.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		tok, ok := r.authenticate(strings.TrimPrefix(auth, "Bearer "))
		if !ok || !tok.HasPermission(requiredPermission(q)) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if supplied := q.Header.Get("X-Tenant-ID"); supplied != "" && supplied != tok.Tenant {
			http.Error(w, "tenant mismatch", http.StatusForbidden)
			return
		}
		ctx := withTenant(withToken(q.Context(), tok), tok.Tenant)
		next.ServeHTTP(w, q.WithContext(ctx))
	})
}
