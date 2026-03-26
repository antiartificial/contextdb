package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/server"
)

// ─── ParseToken ──────────────────────────────────────────────────────────────

func TestParseToken_Valid(t *testing.T) {
	is := is.New(t)

	tok, err := server.ParseToken("acme:read,write:s3cret")
	is.NoErr(err)
	is.Equal(tok.Tenant, "acme")
	is.Equal(len(tok.Permissions), 2)
	is.Equal(tok.Permissions[0], server.PermRead)
	is.Equal(tok.Permissions[1], server.PermWrite)
	is.Equal(tok.Raw, "acme:read,write:s3cret")
}

func TestParseToken_AdminOnly(t *testing.T) {
	is := is.New(t)

	tok, err := server.ParseToken("tenant1:admin:key123")
	is.NoErr(err)
	is.Equal(tok.Tenant, "tenant1")
	is.Equal(len(tok.Permissions), 1)
	is.Equal(tok.Permissions[0], server.PermAdmin)
}

func TestParseToken_EmptyPermsDefaultsToRead(t *testing.T) {
	is := is.New(t)

	tok, err := server.ParseToken("tenant1::secretkey")
	is.NoErr(err)
	is.Equal(len(tok.Permissions), 1)
	is.Equal(tok.Permissions[0], server.PermRead)
}

func TestParseToken_EmptyString(t *testing.T) {
	_, err := server.ParseToken("")
	if err == nil {
		t.Error("expected error for empty token")
	}
}

func TestParseToken_MissingParts(t *testing.T) {
	_, err := server.ParseToken("justonepart")
	if err == nil {
		t.Error("expected error for single-part token")
	}
}

func TestParseToken_TwoParts(t *testing.T) {
	_, err := server.ParseToken("tenant:secret")
	if err == nil {
		t.Error("expected error for two-part token")
	}
}

func TestParseToken_EmptyTenant(t *testing.T) {
	_, err := server.ParseToken(":read:secret")
	if err == nil {
		t.Error("expected error for empty tenant")
	}
}

func TestParseToken_EmptySecret(t *testing.T) {
	_, err := server.ParseToken("tenant:read:")
	if err == nil {
		t.Error("expected error for empty secret")
	}
}

func TestParseToken_UnknownPermission(t *testing.T) {
	_, err := server.ParseToken("tenant:delete:secret")
	if err == nil {
		t.Error("expected error for unknown permission")
	}
}

func TestParseToken_AllPermissions(t *testing.T) {
	is := is.New(t)

	tok, err := server.ParseToken("acme:read,write,admin:key")
	is.NoErr(err)
	is.Equal(len(tok.Permissions), 3)
}

// ─── Token.HasPermission ─────────────────────────────────────────────────────

func TestToken_HasPermission(t *testing.T) {
	is := is.New(t)

	readOnly := server.Token{Permissions: []server.Permission{server.PermRead}}
	is.True(readOnly.HasPermission(server.PermRead))
	is.True(!readOnly.HasPermission(server.PermWrite))
	is.True(!readOnly.HasPermission(server.PermAdmin))

	admin := server.Token{Permissions: []server.Permission{server.PermAdmin}}
	is.True(admin.HasPermission(server.PermRead))  // admin implies all
	is.True(admin.HasPermission(server.PermWrite)) // admin implies all
	is.True(admin.HasPermission(server.PermAdmin))
}

// ─── CanRead / CanWrite / IsAdmin ────────────────────────────────────────────

func TestCanRead_AnonymousAllowed(t *testing.T) {
	is := is.New(t)
	is.True(server.CanRead(server.Token{}, "acme", "ns"))
}

func TestCanRead_MatchingTenant(t *testing.T) {
	is := is.New(t)
	tok := server.Token{Tenant: "acme", Permissions: []server.Permission{server.PermRead}, Raw: "x"}
	is.True(server.CanRead(tok, "acme", "ns"))
}

func TestCanRead_WrongTenant(t *testing.T) {
	is := is.New(t)
	tok := server.Token{Tenant: "acme", Permissions: []server.Permission{server.PermRead}, Raw: "x"}
	is.True(!server.CanRead(tok, "other", "ns"))
}

func TestCanWrite_RequiresPermission(t *testing.T) {
	is := is.New(t)

	readOnly := server.Token{Tenant: "acme", Permissions: []server.Permission{server.PermRead}, Raw: "x"}
	is.True(!server.CanWrite(readOnly, "acme", "ns"))

	writer := server.Token{Tenant: "acme", Permissions: []server.Permission{server.PermWrite}, Raw: "x"}
	is.True(server.CanWrite(writer, "acme", "ns"))
}

func TestIsAdmin_RequiresAdminPerm(t *testing.T) {
	is := is.New(t)

	readOnly := server.Token{Tenant: "acme", Permissions: []server.Permission{server.PermRead}, Raw: "x"}
	is.True(!server.IsAdmin(readOnly, "acme"))

	admin := server.Token{Tenant: "acme", Permissions: []server.Permission{server.PermAdmin}, Raw: "x"}
	is.True(server.IsAdmin(admin, "acme"))
}

func TestIsAdmin_AnonymousNotAdmin(t *testing.T) {
	is := is.New(t)
	is.True(!server.IsAdmin(server.Token{}, "acme"))
}

// ─── RequirePermission ───────────────────────────────────────────────────────

func TestRequirePermission_NoToken_ReadAllowed(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	is.NoErr(server.RequirePermission(ctx, server.PermRead))
}

func TestRequirePermission_NoToken_AdminDenied(t *testing.T) {
	ctx := context.Background()
	err := server.RequirePermission(ctx, server.PermAdmin)
	if err == nil {
		t.Error("expected error for admin without token")
	}
}

func TestRequirePermission_HasPermission(t *testing.T) {
	is := is.New(t)
	tok := server.Token{Permissions: []server.Permission{server.PermWrite}, Raw: "x"}
	// Need to put the token into context via middleware;
	// test via AuthMiddleware round-trip
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := server.RequirePermission(r.Context(), server.PermWrite)
		is.NoErr(err)
		w.WriteHeader(http.StatusOK)
	})
	_ = tok

	// Use AuthMiddleware
	handler := server.AuthMiddleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer acme:write:secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
}

// ─── AuthMiddleware ──────────────────────────────────────────────────────────

func TestAuthMiddleware_NoHeader(t *testing.T) {
	is := is.New(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := server.TokenFromContext(r.Context())
		is.Equal(tok.Raw, "")
		w.WriteHeader(http.StatusOK)
	})

	handler := server.AuthMiddleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	is := is.New(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := server.TokenFromContext(r.Context())
		is.Equal(tok.Tenant, "acme")
		is.Equal(len(tok.Permissions), 2)

		// Tenant should also be set for backward compat
		tenant := server.TenantFromContext(r.Context())
		is.Equal(tenant, "acme")

		w.WriteHeader(http.StatusOK)
	})

	handler := server.AuthMiddleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer acme:read,write:s3cret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusOK)
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	is := is.New(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called for invalid token")
	})

	handler := server.AuthMiddleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer invalid")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusUnauthorized)
}

func TestAuthMiddleware_InvalidScheme(t *testing.T) {
	is := is.New(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called for bad scheme")
	})

	handler := server.AuthMiddleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	is.Equal(w.Code, http.StatusUnauthorized)
}
