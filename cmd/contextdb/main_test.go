package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/buildinfo"
	"github.com/antiartificial/contextdb/pkg/client"
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

func TestWriteBackupMarker(t *testing.T) {
	is := is.New(t)
	path := filepath.Join(t.TempDir(), ".last-backup")
	at := time.Date(2026, 5, 30, 18, 30, 0, 0, time.FixedZone("test", -5*60*60))

	err := writeBackupMarker(path, at)

	is.NoErr(err)
	data, err := os.ReadFile(path)
	is.NoErr(err)
	is.Equal(string(data), "2026-05-30T23:30:00Z\n")
}

func TestBuildSnapshotArtifactManifest(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	backup := filepath.Join(dir, "my-app-20260530T233000Z.ndjson")
	data := []byte(`{"type":"node","data":{"id":"550e8400-e29b-41d4-a716-446655440000"}}
{"type":"edge","data":{"id":"660e8400-e29b-41d4-a716-446655440000"}}
{"type":"source","data":{"id":"docs"}}
`)
	is.NoErr(os.WriteFile(backup, data, 0o644))
	at := time.Date(2026, 5, 30, 18, 30, 0, 0, time.FixedZone("test", -5*60*60))

	manifest, err := buildSnapshotArtifactManifest(snapshotArtifactManifestOptions{
		Namespace:    "my-app",
		BackupPath:   backup,
		BackupMarker: filepath.Join(dir, ".last-backup"),
		CreatedAt:    at,
	})

	is.NoErr(err)
	is.Equal(manifest.SchemaVersion, 1)
	is.Equal(manifest.Namespace, "my-app")
	is.Equal(manifest.BackupFile, "my-app-20260530T233000Z.ndjson")
	is.Equal(manifest.BackupBytes, int64(len(data)))
	is.Equal(manifest.ChecksumSHA256, "0094fb7d1c81b4f5d561d1f90010f07a2abb51dbd6b7506594e0223154d23012")
	is.Equal(manifest.CreatedAt, "2026-05-30T23:30:00Z")
	is.Equal(manifest.ContextDBVersion, buildinfo.Version)
	is.Equal(manifest.Records.Lines, 3)
	is.Equal(manifest.Records.Nodes, 1)
	is.Equal(manifest.Records.Edges, 1)
	is.Equal(manifest.Records.Sources, 1)
}

func TestBuildSnapshotArtifactManifestRequiresFileOutput(t *testing.T) {
	is := is.New(t)

	_, err := buildSnapshotArtifactManifest(snapshotArtifactManifestOptions{
		Namespace:  "my-app",
		BackupPath: "-",
		CreatedAt:  time.Now(),
	})

	is.True(err != nil)
}

func TestVerifySnapshotArtifactManifest(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	backup := filepath.Join(dir, "my-app.ndjson")
	manifestPath := filepath.Join(dir, "my-app.manifest.json")
	data := []byte(`{"type":"node","data":{"id":"550e8400-e29b-41d4-a716-446655440000"}}
`)
	is.NoErr(os.WriteFile(backup, data, 0o644))
	manifest, err := buildSnapshotArtifactManifest(snapshotArtifactManifestOptions{
		Namespace:  "my-app",
		BackupPath: backup,
		CreatedAt:  time.Date(2026, 5, 30, 18, 30, 0, 0, time.UTC),
	})
	is.NoErr(err)
	manifestData, err := json.Marshal(manifest)
	is.NoErr(err)
	is.NoErr(os.WriteFile(manifestPath, manifestData, 0o644))

	report, err := verifySnapshotArtifactManifest(manifestPath, "")

	is.NoErr(err)
	is.True(report.OK)
	is.Equal(report.BackupFile, backup)
	is.Equal(report.ActualBytes, int64(len(data)))
	is.Equal(report.ActualRecords.Nodes, 1)
	is.Equal(len(report.ValidationErrors), 0)
}

func TestVerifySnapshotArtifactManifestRejectsChecksumMismatch(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	backup := filepath.Join(dir, "my-app.ndjson")
	manifestPath := filepath.Join(dir, "my-app.manifest.json")
	is.NoErr(os.WriteFile(backup, []byte(`{"type":"node","data":{}}
`), 0o644))
	manifest, err := buildSnapshotArtifactManifest(snapshotArtifactManifestOptions{
		Namespace:  "my-app",
		BackupPath: backup,
		CreatedAt:  time.Now(),
	})
	is.NoErr(err)
	manifestData, err := json.Marshal(manifest)
	is.NoErr(err)
	is.NoErr(os.WriteFile(manifestPath, manifestData, 0o644))
	is.NoErr(os.WriteFile(backup, []byte(`{"type":"source","data":{}}
`), 0o644))

	report, err := verifySnapshotArtifactManifest(manifestPath, backup)

	is.True(err != nil)
	is.True(!report.OK)
	is.True(len(report.ValidationErrors) > 0)
}

func TestRehearseSnapshotRestore(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	dir := t.TempDir()
	src := client.MustOpen(client.Options{})
	defer src.Close()
	dst := client.MustOpen(client.Options{})
	defer dst.Close()
	ns := src.Namespace("my-app", "general")
	_, err := ns.Write(ctx, client.WriteRequest{
		Content:    "restore rehearsal checks backup before dry-run import",
		SourceID:   "test",
		Confidence: 0.9,
		Vector:     []float32{0.1, 0.2, 0.3},
	})
	is.NoErr(err)
	backup := filepath.Join(dir, "my-app.ndjson")
	manifestPath := filepath.Join(dir, "my-app.manifest.json")
	out, err := os.Create(backup)
	is.NoErr(err)
	is.NoErr(src.ExportSnapshot(ctx, "my-app", out))
	is.NoErr(out.Close())
	is.NoErr(writeSnapshotArtifactManifest(manifestPath, snapshotArtifactManifestOptions{
		Namespace:  "my-app",
		BackupPath: backup,
		CreatedAt:  time.Now(),
	}))

	report, err := rehearseSnapshotRestore(ctx, dst, "restore-preview", manifestPath, "")

	is.NoErr(err)
	is.True(report.OK)
	is.True(report.Verification.OK)
	is.Equal(report.Namespace, "restore-preview")
	is.Equal(report.TargetNamespace, "restore-preview")
	is.True(report.RehearsedAt != "")
	is.Equal(report.RecommendedImportCommand, "contextdb snapshot import --namespace 'restore-preview' --in '"+backup+"' --report")
	is.Equal(report.Restore.DryRun, true)
	is.True(report.Restore.Nodes > 0)
	is.True(report.Restore.NewNodes > 0)
}

func TestRecommendedSnapshotImportCommandQuotesValues(t *testing.T) {
	is := is.New(t)

	command := recommendedSnapshotImportCommand("restore preview", "/tmp/aaron's backup.ndjson")

	is.Equal(command, "contextdb snapshot import --namespace 'restore preview' --in '/tmp/aaron'\"'\"'s backup.ndjson' --report")
}

func TestBuildSnapshotPromotionReceipt(t *testing.T) {
	is := is.New(t)
	at := time.Date(2026, 5, 30, 18, 30, 0, 0, time.FixedZone("test", -5*60*60))

	receipt := buildSnapshotPromotionReceipt(snapshotPromotionReceiptOptions{
		Namespace:  "prod",
		BackupPath: " backup.ndjson ",
		Note:       " promoted after rehearsal ",
		ImportedAt: at,
		Report: client.SnapshotReport{
			Namespace: "prod",
			Nodes:     3,
			NewNodes:  2,
		},
	})

	is.Equal(receipt.SchemaVersion, 1)
	is.Equal(receipt.Namespace, "prod")
	is.Equal(receipt.BackupFile, "backup.ndjson")
	is.Equal(receipt.PromotedAt, "2026-05-30T23:30:00Z")
	is.Equal(receipt.ContextDBVersion, buildinfo.Version)
	is.Equal(receipt.PromotionNote, "promoted after rehearsal")
	is.Equal(receipt.ImportReport.Nodes, 3)
	is.Equal(receipt.ImportReport.NewNodes, 2)
}

func TestWriteSnapshotPromotionReceipt(t *testing.T) {
	is := is.New(t)
	path := filepath.Join(t.TempDir(), "promotion.json")

	err := writeSnapshotPromotionReceipt(path, snapshotPromotionReceiptOptions{
		Namespace:  "prod",
		BackupPath: "backup.ndjson",
		ImportedAt: time.Date(2026, 5, 30, 23, 30, 0, 0, time.UTC),
		Report:     client.SnapshotReport{Namespace: "prod", Nodes: 1},
	})

	is.NoErr(err)
	data, err := os.ReadFile(path)
	is.NoErr(err)
	var receipt snapshotPromotionReceipt
	is.NoErr(json.Unmarshal(data, &receipt))
	is.Equal(receipt.Namespace, "prod")
	is.Equal(receipt.ImportReport.Nodes, 1)
}

func TestVerifySnapshotPromotionReceipt(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "backup.manifest.json")
	receiptPath := filepath.Join(dir, "promotion.json")
	manifest := snapshotArtifactManifest{
		SchemaVersion:    1,
		Namespace:        "source",
		BackupFile:       "backup.ndjson",
		BackupBytes:      100,
		ChecksumSHA256:   "abc",
		CreatedAt:        "2026-05-30T23:00:00Z",
		ContextDBVersion: buildinfo.Version,
		Records:          snapshotArtifactCounts{Lines: 3, Nodes: 2, Edges: 1},
	}
	manifestData, err := json.Marshal(manifest)
	is.NoErr(err)
	is.NoErr(os.WriteFile(manifestPath, manifestData, 0o644))
	receipt := buildSnapshotPromotionReceipt(snapshotPromotionReceiptOptions{
		Namespace:  "prod",
		BackupPath: filepath.Join(dir, "backup.ndjson"),
		ImportedAt: time.Date(2026, 5, 30, 23, 30, 0, 0, time.UTC),
		Report: client.SnapshotReport{
			Namespace: "prod",
			Lines:     3,
			Nodes:     2,
			Edges:     1,
		},
	})
	receiptData, err := json.Marshal(receipt)
	is.NoErr(err)
	is.NoErr(os.WriteFile(receiptPath, receiptData, 0o644))

	report, err := verifySnapshotPromotionReceipt(receiptPath, manifestPath)

	is.NoErr(err)
	is.True(report.OK)
	is.Equal(report.ReceiptNamespace, "prod")
	is.Equal(report.ImportNamespace, "prod")
	is.Equal(report.ImportedRecords.Nodes, 2)
	is.Equal(report.ManifestRecords.Nodes, 2)
}

func TestVerifySnapshotPromotionReceiptRejectsRecordMismatch(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "backup.manifest.json")
	receiptPath := filepath.Join(dir, "promotion.json")
	manifestData, err := json.Marshal(snapshotArtifactManifest{
		SchemaVersion:    1,
		BackupFile:       "backup.ndjson",
		ContextDBVersion: buildinfo.Version,
		Records:          snapshotArtifactCounts{Lines: 2, Nodes: 2},
	})
	is.NoErr(err)
	is.NoErr(os.WriteFile(manifestPath, manifestData, 0o644))
	receiptData, err := json.Marshal(buildSnapshotPromotionReceipt(snapshotPromotionReceiptOptions{
		Namespace:  "prod",
		BackupPath: filepath.Join(dir, "backup.ndjson"),
		ImportedAt: time.Now(),
		Report:     client.SnapshotReport{Namespace: "prod", Lines: 1, Nodes: 1},
	}))
	is.NoErr(err)
	is.NoErr(os.WriteFile(receiptPath, receiptData, 0o644))

	report, err := verifySnapshotPromotionReceipt(receiptPath, manifestPath)

	is.True(err != nil)
	is.True(!report.OK)
	is.True(len(report.ValidationErrors) > 0)
}

func TestVerifySnapshotLifecycleSummaryWithoutPromotion(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup.ndjson")
	manifestPath := filepath.Join(dir, "backup.manifest.json")
	rehearsalPath := filepath.Join(dir, "backup.rehearsal.json")
	summaryPath := filepath.Join(dir, "backup.lifecycle.json")
	data := []byte(`{"type":"node","data":{"id":"550e8400-e29b-41d4-a716-446655440000"}}
`)
	is.NoErr(os.WriteFile(backup, data, 0o644))
	is.NoErr(writeSnapshotArtifactManifest(manifestPath, snapshotArtifactManifestOptions{
		Namespace:  "prod",
		BackupPath: backup,
		CreatedAt:  time.Date(2026, 5, 30, 23, 30, 0, 0, time.UTC),
	}))
	rehearsalData, err := json.Marshal(snapshotRehearsalReport{
		OK:              true,
		Namespace:       "prod-restore-preview",
		RehearsedAt:     "2026-05-30T23:31:00Z",
		TargetNamespace: "prod-restore-preview",
		Verification:    snapshotArtifactVerifyReport{OK: true},
	})
	is.NoErr(err)
	is.NoErr(os.WriteFile(rehearsalPath, rehearsalData, 0o644))
	summaryData, err := json.Marshal(snapshotLifecycleSummary{
		Namespace:    "prod",
		CreatedAt:    "2026-05-30T23:32:00Z",
		Backup:       backup,
		Manifest:     manifestPath,
		Rehearsal:    rehearsalPath,
		Promotion:    filepath.Join(dir, "backup.promotion.json"),
		ReceiptCheck: filepath.Join(dir, "backup.receipt-check.json"),
		Promoted:     false,
	})
	is.NoErr(err)
	is.NoErr(os.WriteFile(summaryPath, summaryData, 0o644))

	report, err := verifySnapshotLifecycleSummary(summaryPath)

	is.NoErr(err)
	is.True(report.OK)
	is.Equal(report.Namespace, "prod")
	is.True(report.BackupExists)
	is.True(report.ManifestOK)
	is.True(report.RehearsalOK)
	is.True(!report.PromotionOK)
	is.Equal(len(report.ValidationErrors), 0)
}

func TestVerifySnapshotLifecycleSummaryWithPromotion(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup.ndjson")
	manifestPath := filepath.Join(dir, "backup.manifest.json")
	rehearsalPath := filepath.Join(dir, "backup.rehearsal.json")
	promotionPath := filepath.Join(dir, "backup.promotion.json")
	receiptCheckPath := filepath.Join(dir, "backup.receipt-check.json")
	summaryPath := filepath.Join(dir, "backup.lifecycle.json")
	data := []byte(`{"type":"node","data":{"id":"550e8400-e29b-41d4-a716-446655440000"}}
{"type":"source","data":{"id":"docs"}}
`)
	is.NoErr(os.WriteFile(backup, data, 0o644))
	is.NoErr(writeSnapshotArtifactManifest(manifestPath, snapshotArtifactManifestOptions{
		Namespace:  "prod",
		BackupPath: backup,
		CreatedAt:  time.Date(2026, 5, 30, 23, 30, 0, 0, time.UTC),
	}))
	rehearsalData, err := json.Marshal(snapshotRehearsalReport{
		OK:              true,
		Namespace:       "prod-restore-preview",
		RehearsedAt:     "2026-05-30T23:31:00Z",
		TargetNamespace: "prod-restore-preview",
		Verification:    snapshotArtifactVerifyReport{OK: true},
	})
	is.NoErr(err)
	is.NoErr(os.WriteFile(rehearsalPath, rehearsalData, 0o644))
	receipt := buildSnapshotPromotionReceipt(snapshotPromotionReceiptOptions{
		Namespace:  "prod",
		BackupPath: backup,
		ImportedAt: time.Date(2026, 5, 30, 23, 32, 0, 0, time.UTC),
		Report: client.SnapshotReport{
			Namespace: "prod",
			Lines:     2,
			Nodes:     1,
			Sources:   1,
		},
	})
	promotionData, err := json.Marshal(receipt)
	is.NoErr(err)
	is.NoErr(os.WriteFile(promotionPath, promotionData, 0o644))
	receiptCheckData, err := json.Marshal(snapshotPromotionReceiptVerifyReport{OK: true})
	is.NoErr(err)
	is.NoErr(os.WriteFile(receiptCheckPath, receiptCheckData, 0o644))
	summaryData, err := json.Marshal(snapshotLifecycleSummary{
		Namespace:    "prod",
		CreatedAt:    "2026-05-30T23:33:00Z",
		Backup:       backup,
		Manifest:     manifestPath,
		Rehearsal:    rehearsalPath,
		Promotion:    promotionPath,
		ReceiptCheck: receiptCheckPath,
		Promoted:     true,
	})
	is.NoErr(err)
	is.NoErr(os.WriteFile(summaryPath, summaryData, 0o644))

	report, err := verifySnapshotLifecycleSummary(summaryPath)

	is.NoErr(err)
	is.True(report.OK)
	is.True(report.PromotionExists)
	is.True(report.PromotionOK)
	is.True(report.ReceiptCheckOK)
}

func TestVerifySnapshotLifecycleSummaryRejectsBadReceiptCheck(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup.ndjson")
	manifestPath := filepath.Join(dir, "backup.manifest.json")
	rehearsalPath := filepath.Join(dir, "backup.rehearsal.json")
	promotionPath := filepath.Join(dir, "backup.promotion.json")
	receiptCheckPath := filepath.Join(dir, "backup.receipt-check.json")
	summaryPath := filepath.Join(dir, "backup.lifecycle.json")
	is.NoErr(os.WriteFile(backup, []byte(`{"type":"node","data":{}}
`), 0o644))
	is.NoErr(writeSnapshotArtifactManifest(manifestPath, snapshotArtifactManifestOptions{
		Namespace:  "prod",
		BackupPath: backup,
		CreatedAt:  time.Now(),
	}))
	rehearsalData, err := json.Marshal(snapshotRehearsalReport{OK: true, Verification: snapshotArtifactVerifyReport{OK: true}})
	is.NoErr(err)
	is.NoErr(os.WriteFile(rehearsalPath, rehearsalData, 0o644))
	promotionData, err := json.Marshal(buildSnapshotPromotionReceipt(snapshotPromotionReceiptOptions{
		Namespace:  "prod",
		BackupPath: backup,
		ImportedAt: time.Now(),
		Report:     client.SnapshotReport{Namespace: "prod", Lines: 1, Nodes: 1},
	}))
	is.NoErr(err)
	is.NoErr(os.WriteFile(promotionPath, promotionData, 0o644))
	receiptCheckData, err := json.Marshal(snapshotPromotionReceiptVerifyReport{
		OK:               false,
		ValidationErrors: []string{"record counts mismatch"},
	})
	is.NoErr(err)
	is.NoErr(os.WriteFile(receiptCheckPath, receiptCheckData, 0o644))
	summaryData, err := json.Marshal(snapshotLifecycleSummary{
		Namespace:    "prod",
		CreatedAt:    "2026-05-30T23:33:00Z",
		Backup:       backup,
		Manifest:     manifestPath,
		Rehearsal:    rehearsalPath,
		Promotion:    promotionPath,
		ReceiptCheck: receiptCheckPath,
		Promoted:     true,
	})
	is.NoErr(err)
	is.NoErr(os.WriteFile(summaryPath, summaryData, 0o644))

	report, err := verifySnapshotLifecycleSummary(summaryPath)

	is.True(err != nil)
	is.True(!report.OK)
	is.True(!report.ReceiptCheckOK)
	is.True(len(report.ValidationErrors) > 0)
}

func TestBuildSnapshotLifecycleRetentionReport(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	writeLifecycleFixture(t, dir, "prod", "2026-05-31T23:30:00Z", true)
	writeLifecycleFixture(t, dir, "prod", "2026-06-01T23:30:00Z", false)

	report, err := buildSnapshotLifecycleRetentionReport(dir, "prod", 2)

	is.NoErr(err)
	is.True(report.OK)
	is.Equal(report.TotalBundles, 3)
	is.Equal(report.KeepBundles, 2)
	is.Equal(report.PruneableBundles, 1)
	is.Equal(report.Bundles[0].Decision, "keep")
	is.Equal(report.Bundles[0].CreatedAt, "2026-06-01T23:30:00Z")
	is.Equal(report.Bundles[1].Decision, "keep")
	is.Equal(report.Bundles[2].Decision, "pruneable")
	is.Equal(len(report.Bundles[0].Artifacts), 6)
	is.True(report.Bundles[0].Artifacts[0].Exists)
	is.Equal(report.Bundles[0].Artifacts[0].Kind, "summary")
	is.True(len(report.DeleteCommands) > 0)
}

func TestBuildSnapshotLifecycleRetentionReportFiltersNamespace(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	writeLifecycleFixture(t, dir, "staging", "2026-05-31T23:30:00Z", false)

	report, err := buildSnapshotLifecycleRetentionReport(dir, "prod", 1)

	is.NoErr(err)
	is.True(report.OK)
	is.Equal(report.TotalBundles, 1)
	is.Equal(report.Bundles[0].Namespace, "prod")
}

func TestBuildSnapshotLifecycleRetentionReportRejectsInvalidKeep(t *testing.T) {
	is := is.New(t)

	report, err := buildSnapshotLifecycleRetentionReport(t.TempDir(), "", 0)

	is.True(err != nil)
	is.True(!report.OK)
	is.True(len(report.ValidationErrors) > 0)
}

func TestBuildSnapshotLifecycleDeleteScript(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	writeLifecycleFixture(t, dir, "prod", "2026-05-31T23:30:00Z", false)

	report, err := buildSnapshotLifecycleRetentionReport(dir, "prod", 1)
	is.NoErr(err)
	script := buildSnapshotLifecycleDeleteScript(report)

	is.True(strings.HasPrefix(script, "#!/usr/bin/env bash\n"))
	is.True(strings.Contains(script, "rm -- "))
	is.True(strings.Contains(script, "prod-20260530T233000.lifecycle.json"))
	is.True(!strings.Contains(script, "prod-20260531T233000.lifecycle.json"))
}

func TestWriteSnapshotLifecycleIndex(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	writeLifecycleFixture(t, dir, "prod", "2026-05-31T23:30:00Z", false)
	out := filepath.Join(dir, "contextdb-backups.index.json")

	index, err := writeSnapshotLifecycleIndex(out, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})

	is.NoErr(err)
	is.Equal(index.SchemaVersion, 1)
	is.Equal(index.IndexFile, out)
	is.Equal(index.GeneratedAt, "2026-06-01T12:00:00Z")
	is.Equal(index.TotalBundles, 2)
	is.Equal(index.KeepBundles, 1)
	is.Equal(index.PruneableBundles, 1)
	is.True(len(index.DeleteCommands) > 0)
	is.True(index.Bundles[0].Artifacts[0].ChecksumSHA256 != "")
	data, err := os.ReadFile(out)
	is.NoErr(err)
	var decoded snapshotLifecycleIndex
	is.NoErr(json.Unmarshal(data, &decoded))
	is.Equal(decoded.TotalBundles, 2)
}

func TestBuildSnapshotLifecycleIndexUsesDefaultPath(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)

	index, err := buildSnapshotLifecycleIndex("", snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})

	is.NoErr(err)
	is.Equal(index.IndexFile, filepath.Join(dir, "contextdb-backups.index.json"))
	is.Equal(index.TotalBundles, 1)
}

func writeLifecycleFixture(t *testing.T, dir, namespace, createdAt string, promoted bool) {
	t.Helper()
	stamp := strings.NewReplacer("-", "", ":", "").Replace(createdAt[:19])
	prefix := filepath.Join(dir, namespace+"-"+stamp)
	files := map[string]string{
		"backup":    prefix + ".ndjson",
		"manifest":  prefix + ".manifest.json",
		"rehearsal": prefix + ".rehearsal.json",
	}
	if promoted {
		files["promotion"] = prefix + ".promotion.json"
		files["receipt_check"] = prefix + ".receipt-check.json"
	}
	for _, path := range files {
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	summary := snapshotLifecycleSummary{
		Namespace:    namespace,
		CreatedAt:    createdAt,
		Backup:       files["backup"],
		Manifest:     files["manifest"],
		Rehearsal:    files["rehearsal"],
		Promotion:    files["promotion"],
		ReceiptCheck: files["receipt_check"],
		Promoted:     promoted,
	}
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prefix+".lifecycle.json", data, 0o644); err != nil {
		t.Fatal(err)
	}
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
