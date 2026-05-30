package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/buildinfo"
)

func TestBuildNornManifestEntry(t *testing.T) {
	is := is.New(t)

	entry, err := buildNornManifestEntry(nornManifestOptions{
		App:         "contextdb",
		Name:        "contextdb-mini",
		Endpoint:    "https://contextdb.example.test/",
		GRPCAddr:    ":7700",
		RESTAddr:    "127.0.0.1:8801",
		ObserveAddr: ":9902",
		Tags:        []string{"contextdb", "rest"},
	})
	is.NoErr(err)
	is.Equal(entry.App, "contextdb")
	is.Equal(entry.Name, "contextdb-mini")
	is.Equal(entry.Version, buildinfo.Version)
	is.Equal(entry.Endpoint, "https://contextdb.example.test")
	is.Equal(entry.HealthURL, "https://contextdb.example.test/v1/ping")
	is.Equal(entry.GraphQLURL, "https://contextdb.example.test/graphql")
	is.Equal(entry.FeaturesURL, "https://contextdb.example.test/v1/features")
	is.Equal(entry.Ports.GRPC, 7700)
	is.Equal(entry.Ports.REST, 8801)
	is.Equal(entry.Ports.Observe, 9902)
	is.Equal(len(entry.Tags), 2)
}

func TestDefaultNornEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		publicURL string
		restAddr  string
		want      string
	}{
		{
			name: "default local rest port",
			want: "http://127.0.0.1:7701",
		},
		{
			name:     "host port rest address",
			restAddr: "127.0.0.1:8801",
			want:     "http://127.0.0.1:8801",
		},
		{
			name:     "absolute rest URL",
			restAddr: "https://contextdb.example.test",
			want:     "https://contextdb.example.test",
		},
		{
			name:      "public URL override",
			publicURL: "https://public.contextdb.example.test",
			restAddr:  "127.0.0.1:8801",
			want:      "https://public.contextdb.example.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			t.Setenv("CONTEXTDB_PUBLIC_URL", tt.publicURL)
			t.Setenv("CONTEXTDB_REST_ADDR", tt.restAddr)

			is.Equal(defaultNornEndpoint(), tt.want)
		})
	}
}

func TestParseUUIDList(t *testing.T) {
	is := is.New(t)

	ids, err := parseUUIDList("550e8400-e29b-41d4-a716-446655440000,550e8400-e29b-41d4-a716-446655440001")

	is.NoErr(err)
	is.Equal(len(ids), 2)
	is.Equal(ids[0].String(), "550e8400-e29b-41d4-a716-446655440000")
	is.Equal(ids[1].String(), "550e8400-e29b-41d4-a716-446655440001")
}

func TestParseUUIDListRejectsInvalidSeed(t *testing.T) {
	is := is.New(t)

	_, err := parseUUIDList("not-a-uuid")

	is.True(err != nil)
}

func TestBuildNornDriftReportMatches(t *testing.T) {
	is := is.New(t)

	entry := nornManifestEntry{
		App:         "contextdb",
		Name:        "contextdb",
		Version:     buildinfo.Version,
		Endpoint:    "https://contextdb.example.test",
		HealthURL:   "https://contextdb.example.test/v1/ping",
		GraphQLURL:  "https://contextdb.example.test/graphql",
		FeaturesURL: "https://contextdb.example.test/v1/features",
		Ports:       nornPorts{GRPC: 7700, REST: 7701, Observe: 7702},
		Tags:        []string{"contextdb", "rest", "graphql"},
	}

	report := buildNornDriftReport(entry, entry)

	is.True(report.OK)
	is.Equal(len(report.Diffs), 0)
}

func TestBuildNornDriftReportDetectsFieldDiffs(t *testing.T) {
	is := is.New(t)

	expected := nornManifestEntry{
		App:         "contextdb",
		Name:        "contextdb",
		Version:     buildinfo.Version,
		Endpoint:    "https://contextdb.example.test",
		HealthURL:   "https://contextdb.example.test/v1/ping",
		GraphQLURL:  "https://contextdb.example.test/graphql",
		FeaturesURL: "https://contextdb.example.test/v1/features",
		Ports:       nornPorts{GRPC: 7700, REST: 7701, Observe: 7702},
		Tags:        []string{"contextdb", "rest", "graphql"},
	}
	actual := expected
	actual.Endpoint = "https://old-contextdb.example.test"
	actual.Ports.REST = 8801

	report := buildNornDriftReport(expected, actual)

	is.True(!report.OK)
	is.Equal(len(report.Diffs), 2)
	is.Equal(report.Diffs[0].Field, "endpoint")
	is.Equal(report.Diffs[1].Field, "ports.rest")
}

func TestFetchNornManifestEntryFindsServiceDocumentEntry(t *testing.T) {
	is := is.New(t)

	expected := nornManifestEntry{
		App:         "contextdb",
		Name:        "contextdb",
		Version:     buildinfo.Version,
		Endpoint:    "https://contextdb.example.test",
		HealthURL:   "https://contextdb.example.test/v1/ping",
		GraphQLURL:  "https://contextdb.example.test/graphql",
		FeaturesURL: "https://contextdb.example.test/v1/features",
		Ports:       nornPorts{GRPC: 7700, REST: 7701, Observe: 7702},
		Tags:        []string{"contextdb", "rest", "graphql"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		is.Equal(r.Method, http.MethodGet)
		_ = json.NewEncoder(w).Encode(nornManifestDocument{
			Services: []nornManifestEntry{
				{App: "other", Name: "other"},
				expected,
			},
		})
	}))
	defer srv.Close()

	actual, err := fetchNornManifestEntry(context.Background(), srv.URL, "contextdb", "contextdb")

	is.NoErr(err)
	is.Equal(actual.Endpoint, expected.Endpoint)
	is.Equal(actual.Ports.REST, expected.Ports.REST)
}

func TestValidateNornManifestEntryRejectsWrongApp(t *testing.T) {
	is := is.New(t)

	err := validateNornManifestEntry(nornManifestEntry{
		App:      "other",
		Name:     "contextdb",
		Endpoint: "https://contextdb.example.test",
		Ports:    nornPorts{REST: 7701},
	})
	is.True(err != nil)
}

func TestValidateNornManifestEntryRejectsRelativeEndpoint(t *testing.T) {
	is := is.New(t)

	err := validateNornManifestEntry(nornManifestEntry{
		App:      "contextdb",
		Name:     "contextdb",
		Endpoint: "/contextdb",
		Ports:    nornPorts{REST: 7701},
	})
	is.True(err != nil)
}
