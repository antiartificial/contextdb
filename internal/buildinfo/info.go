package buildinfo

const (
	// Version is the current contextdb release version.
	Version = "0.49.0"
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
		ReleaseNotesPath: "/contextdb/releases/v0.49.0",
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
		{Name: "doctor-store-consistency", Status: "stable", Since: "v0.49.0", Description: "Opt-in local doctor check samples graph nodes and reports vector rebuild candidates."},
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
		{Name: "norn-live-drift-check", Status: "stable", Since: "v0.16.0", Description: "CLI drift check that compares the expected contextdb Norn manifest entry with the live Norn manifest."},
		{Name: "snapshot-backup-restore", Status: "stable", Since: "v0.17.0", Description: "Public snapshot export/import helpers and CLI backup/restore commands with dry-run validation."},
		{Name: "snapshot-restore-report", Status: "stable", Since: "v0.18.0", Description: "Snapshot dry-run and import reports summarize processed lines, records, vectors, and namespace overrides."},
		{Name: "snapshot-backup-marker", Status: "stable", Since: "v0.19.0", Description: "Snapshot export can write a backup marker after a successful backup for doctor readiness checks."},
		{Name: "snapshot-diff-preview", Status: "stable", Since: "v0.20.0", Description: "Snapshot restore reports include new, changed, and unchanged node counts for previewing imports."},
		{Name: "backup-runbook", Status: "stable", Since: "v0.21.0", Description: "Documented backup workflow for scheduled snapshot export, restore preview, marker checks, and Norn pairing."},
		{Name: "backup-artifact-manifest", Status: "stable", Since: "v0.22.0", Description: "Snapshot export can write a checksummed JSON sidecar with backup metadata and record counts."},
		{Name: "backup-manifest-verify", Status: "stable", Since: "v0.23.0", Description: "Snapshot verify checks a backup file against its artifact manifest checksum, size, and record counts."},
		{Name: "restore-rehearsal", Status: "stable", Since: "v0.24.0", Description: "Snapshot rehearse verifies a backup artifact and runs a dry-run restore report in one preflight command."},
		{Name: "restore-promotion-checklist", Status: "stable", Since: "v0.25.0", Description: "Snapshot rehearsal reports include promotion metadata and a recommended import command."},
		{Name: "restore-promotion-receipt", Status: "stable", Since: "v0.26.0", Description: "Snapshot import can write a JSON promotion receipt with operator note and import counts."},
		{Name: "promotion-receipt-verify", Status: "stable", Since: "v0.27.0", Description: "Snapshot receipt verification compares promotion receipts against artifact manifests."},
		{Name: "backup-lifecycle-bundle", Status: "stable", Since: "v0.28.0", Description: "Backup runbook includes a guarded lifecycle script for export, verify, rehearse, optional promote, receipt verify, and summary output."},
		{Name: "lifecycle-summary-verify", Status: "stable", Since: "v0.29.0", Description: "Snapshot lifecycle verification checks a lifecycle summary and its referenced backup, manifest, rehearsal, promotion, and receipt-check artifacts."},
		{Name: "lifecycle-retention-report", Status: "stable", Since: "v0.30.0", Description: "Snapshot lifecycle retention reports group backup bundles and mark newest artifacts to keep versus older pruneable bundles without deleting files."},
		{Name: "lifecycle-delete-plan", Status: "stable", Since: "v0.31.0", Description: "Snapshot lifecycle retention can emit a reviewed shell deletion plan for pruneable artifacts without deleting files."},
		{Name: "lifecycle-manifest-index", Status: "stable", Since: "v0.32.0", Description: "Snapshot lifecycle index writes a compact JSON catalog of backup bundles, retention decisions, artifact sizes, and hashes."},
		{Name: "lifecycle-index-verify", Status: "stable", Since: "v0.33.0", Description: "Snapshot lifecycle index verification re-checks indexed artifact existence, sizes, and hashes."},
		{Name: "lifecycle-index-diff", Status: "stable", Since: "v0.34.0", Description: "Snapshot lifecycle index diff compares backup catalogs across runs or hosts for bundle and artifact changes."},
		{Name: "norn-manifest-publish", Status: "stable", Since: "v0.35.0", Description: "Norn manifest publish validates a dry-run plan by default and can explicitly publish the service entry to a configured Norn endpoint."},
		{Name: "lifecycle-index-publish", Status: "stable", Since: "v0.36.0", Description: "Snapshot lifecycle index publish validates and optionally sends backup catalog metadata to a configured ops endpoint without uploading backup contents."},
		{Name: "lifecycle-index-publish-drift", Status: "stable", Since: "v0.47.0", Description: "Snapshot lifecycle index publish drift compares local backup catalog metadata with the published ops payload."},
		{Name: "ranking-eval-snapshots", Status: "stable", Since: "v0.48.0", Description: "Ranking eval snapshots emit JSON score-drift reports for the representative corpus."},
		{Name: "review-escalation-rules", Status: "stable", Since: "v0.37.0", Description: "Review queue escalation metadata flags aged assigned or snoozed items and high-priority source anomaly tasks."},
		{Name: "review-escalation-digest", Status: "stable", Since: "v0.38.0", Description: "Review escalation digests summarize escalated queue items by owner, source, item type, and escalation level."},
		{Name: "review-escalation-digest-export", Status: "stable", Since: "v0.39.0", Description: "Review escalation digest export records durable digest snapshots for review handoffs."},
		{Name: "review-handoff-feed", Status: "stable", Since: "v0.40.0", Description: "Review handoff feeds expose saved escalation digest snapshots filtered by owner and escalation level."},
		{Name: "review-handoff-webhook-plan", Status: "stable", Since: "v0.41.0", Description: "Review handoff webhook plans produce signed dry-run delivery payloads for saved escalation handoffs."},
		{Name: "review-handoff-webhook-execution", Status: "stable", Since: "v0.42.0", Description: "Review handoff webhook execution sends opt-in handoff deliveries with timeout and response capture."},
		{Name: "review-handoff-delivery-receipts", Status: "stable", Since: "v0.43.0", Description: "Review handoff delivery receipts record append-only webhook delivery audit events."},
		{Name: "review-handoff-retry-candidates", Status: "stable", Since: "v0.44.0", Description: "Review handoff retry candidates group unresolved failed webhook delivery receipts without sending retries."},
		{Name: "review-handoff-retry-execution", Status: "stable", Since: "v0.45.0", Description: "Review handoff retry execution resends unresolved failed handoff deliveries with explicit operator control."},
		{Name: "review-handoff-retry-backoff", Status: "stable", Since: "v0.46.0", Description: "Review handoff retry backoff recommendations provide read-only pacing guidance from delivery receipt history."},
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
