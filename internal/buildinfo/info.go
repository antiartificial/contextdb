package buildinfo

const (
	// Version is the current contextdb release version.
	Version = "0.15.0"
)

type Feature struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Since       string `json:"since"`
	Description string `json:"description"`
}

type Migration struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
}

type Info struct {
	Version          string      `json:"version"`
	APIVersion       string      `json:"api_version"`
	DocsVersion      string      `json:"docs_version"`
	Compatibility    string      `json:"compatibility"`
	LatestMigration  int         `json:"latest_migration"`
	Features         []Feature   `json:"features"`
	Migrations       []Migration `json:"migrations"`
	RecommendedDocs  string      `json:"recommended_docs"`
	ReleaseNotesPath string      `json:"release_notes_path"`
}

func Current(migrations []Migration) Info {
	return Info{
		Version:          Version,
		APIVersion:       "v1",
		DocsVersion:      Version,
		Compatibility:    "non-breaking pre-1.0 minor release",
		LatestMigration:  latestMigration(migrations),
		Features:         Features(),
		Migrations:       migrations,
		RecommendedDocs:  "/contextdb/",
		ReleaseNotesPath: "/contextdb/releases/v0.15.0",
	}
}

func Features() []Feature {
	return []Feature{
		{Name: "go-sdk", Status: "stable", Since: "v0.1.0", Description: "Embedded and remote Go client APIs for write, retrieve, feedback, history, and import/export."},
		{Name: "rest-api", Status: "stable", Since: "v0.1.0", Description: "HTTP API for namespace writes, retrieval, feedback, narrative reports, gaps, stats, ping, and introspection."},
		{Name: "grpc-api", Status: "stable", Since: "v0.1.0", Description: "JSON-over-gRPC API for public operations and remote store access."},
		{Name: "graphql-api", Status: "stable", Since: "v0.3.0", Description: "GraphQL search, feedback, narrative, knowledge gap, and introspection queries."},
		{Name: "embedded-badger", Status: "stable", Since: "v0.3.0", Description: "Persistent embedded graph, KV, event, and vector storage backed by Badger."},
		{Name: "postgres-standard-mode", Status: "beta", Since: "v0.1.0", Description: "Postgres-backed standard mode with schema migrations and vector retrieval."},
		{Name: "feedback-epistemics", Status: "stable", Since: "v0.2.0", Description: "Validate, refute, useful, and stale feedback updates node versions, utility, SM-2 metadata, and source credibility."},
		{Name: "narrative-and-gaps", Status: "stable", Since: "v0.3.0", Description: "Narrative explanations and knowledge gap detection for inspectable retrieval."},
		{Name: "feature-introspection", Status: "stable", Since: "v0.4.0", Description: "REST and GraphQL version, feature, and migration discovery endpoints."},
		{Name: "doctor-sample-write", Status: "stable", Since: "v0.4.1", Description: "Opt-in doctor write/retrieve probe for live REST deployments."},
		{Name: "feedback-event-log", Status: "stable", Since: "v0.5.0", Description: "Durable feedback audit events exposed through the Go SDK, REST, and GraphQL."},
		{Name: "source-trust-timeline", Status: "stable", Since: "v0.6.0", Description: "Source credibility timeline points derived from durable feedback events."},
		{Name: "claim-review-queue", Status: "stable", Since: "v0.7.0", Description: "Derived review tasks for refuted, stale, low-confidence, and contradictory claims."},
		{Name: "explain-rank", Status: "stable", Since: "v0.8.0", Description: "Compare two nodes and explain ranking differences with score component deltas."},
		{Name: "knowledge-acquisition-planner", Status: "stable", Since: "v0.9.0", Description: "Convert knowledge gaps and weak claims into prioritized source-backed acquisition tasks."},
		{Name: "doctor-backup-readiness", Status: "stable", Since: "v0.10.0", Description: "Opt-in doctor check for recent backup marker evidence."},
		{Name: "explain-rank-graph-evidence", Status: "stable", Since: "v0.11.0", Description: "Support-chain evidence and compound confidence in rank explanations."},
		{Name: "release-health-page", Status: "stable", Since: "v0.11.2", Description: "Release gate summary for unit, docs, ranking, durability, API contract, and race/soak checks."},
		{Name: "review-workflow-persistence", Status: "stable", Since: "v0.12.0", Description: "Append-only review decisions for assignment, status, resolution notes, and re-check scheduling."},
		{Name: "source-trust-anomaly-alerts", Status: "stable", Since: "v0.13.0", Description: "Review queue tasks for source credibility drops, low trust thresholds, and repeated refutations."},
		{Name: "norn-registration-helper", Status: "stable", Since: "v0.14.0", Description: "CLI helper to generate and validate contextdb Norn manifest entries."},
		{Name: "review-queue-filters", Status: "stable", Since: "v0.15.0", Description: "Review queue filters for task type, source, workflow status, and owner across Go SDK, REST, and GraphQL."},
	}
}

func latestMigration(migrations []Migration) int {
	latest := 0
	for _, migration := range migrations {
		if migration.Version > latest {
			latest = migration.Version
		}
	}
	return latest
}
