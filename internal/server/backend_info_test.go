package server

import (
	"encoding/json"
	"github.com/antiartificial/contextdb/pkg/client"
	"net/http/httptest"
	"testing"
)

func TestBackendIntrospection(t *testing.T) {
	db, err := client.Open(client.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w := httptest.NewRecorder()
	NewRESTServer(db).Handler().ServeHTTP(w, httptest.NewRequest("GET", "/v1/backends", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var info client.BackendInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Mode != client.ModeEmbedded || info.Graph != "memory" || info.Persistent {
		t.Fatalf("unexpected %+v", info)
	}
}
