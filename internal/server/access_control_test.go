package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenRegistryRequiresConfiguredExactTokenAndPermissions(t *testing.T) {
	r, err := NewTokenRegistry(`["acme:read:secret","acme:write:writer"]`)
	if err != nil {
		t.Fatal(err)
	}
	h := r.Middleware(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		auth, method, tenant string
		want                 int
	}{{"", "GET", "", 401}, {"Bearer acme:read:fake", "GET", "", 403}, {"Bearer acme:read:secret", "POST", "", 403}, {"Bearer acme:write:writer", "POST", "other", 403}, {"Bearer acme:write:writer", "POST", "acme", 204}} {
		q := httptest.NewRequest(tc.method, "/v1/namespaces/x/write", nil)
		q.Header.Set("Authorization", tc.auth)
		q.Header.Set("X-Tenant-ID", tc.tenant)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, q)
		if w.Code != tc.want {
			t.Fatalf("%s %s: got %d want %d", tc.auth, tc.method, w.Code, tc.want)
		}
	}
}
