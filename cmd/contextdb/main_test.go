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

	"github.com/google/uuid"
	"github.com/matryer/is"

	"github.com/antiartificial/contextdb/internal/buildinfo"
	"github.com/antiartificial/contextdb/internal/core"
	memstore "github.com/antiartificial/contextdb/internal/store/memory"
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

func TestVerifySnapshotLifecycleIndex(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	out := filepath.Join(dir, "contextdb-backups.index.json")
	_, err := writeSnapshotLifecycleIndex(out, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)

	report, err := verifySnapshotLifecycleIndex(out)

	is.NoErr(err)
	is.True(report.OK)
	is.Equal(report.TotalBundles, 1)
	is.True(report.TotalArtifacts > 0)
	is.Equal(report.VerifiedArtifacts, report.TotalArtifacts)
	is.Equal(len(report.ValidationErrors), 0)
}

func TestVerifySnapshotLifecycleIndexRejectsChecksumMismatch(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	out := filepath.Join(dir, "contextdb-backups.index.json")
	index, err := writeSnapshotLifecycleIndex(out, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)
	var target string
	for _, artifact := range index.Bundles[0].Artifacts {
		if artifact.Kind == "summary" {
			target = artifact.Path
			break
		}
	}
	is.NoErr(os.WriteFile(target, []byte(`{"changed":true}`), 0o644))

	report, err := verifySnapshotLifecycleIndex(out)

	is.True(err != nil)
	is.True(!report.OK)
	is.True(len(report.ValidationErrors) > 0)
}

func TestDiffSnapshotLifecycleIndexesMatches(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	oldPath := filepath.Join(dir, "old.index.json")
	newPath := filepath.Join(dir, "new.index.json")
	_, err := writeSnapshotLifecycleIndex(oldPath, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)
	_, err = writeSnapshotLifecycleIndex(newPath, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)

	report, err := diffSnapshotLifecycleIndexes(oldPath, newPath)

	is.NoErr(err)
	is.True(report.OK)
	is.Equal(report.OldBundles, 1)
	is.Equal(report.NewBundles, 1)
	is.Equal(len(report.ChangedBundles), 0)
}

func TestDiffSnapshotLifecycleIndexesDetectsAddedBundle(t *testing.T) {
	is := is.New(t)
	oldDir := t.TempDir()
	newDir := t.TempDir()
	writeLifecycleFixture(t, oldDir, "prod", "2026-05-30T23:30:00Z", false)
	writeLifecycleFixture(t, newDir, "prod", "2026-05-30T23:30:00Z", false)
	writeLifecycleFixture(t, newDir, "prod", "2026-05-31T23:30:00Z", false)
	oldPath := filepath.Join(oldDir, "contextdb-backups.index.json")
	newPath := filepath.Join(newDir, "contextdb-backups.index.json")
	_, err := writeSnapshotLifecycleIndex(oldPath, snapshotLifecycleIndexOptions{
		Dir:       oldDir,
		Namespace: "prod",
		Keep:      2,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)
	_, err = writeSnapshotLifecycleIndex(newPath, snapshotLifecycleIndexOptions{
		Dir:       newDir,
		Namespace: "prod",
		Keep:      2,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)

	report, err := diffSnapshotLifecycleIndexes(oldPath, newPath)

	is.True(err != nil)
	is.True(!report.OK)
	is.Equal(len(report.AddedBundles), 1)
	is.True(strings.Contains(report.AddedBundles[0], "2026-05-31T23:30:00Z"))
}

func TestDiffSnapshotLifecycleIndexesDetectsArtifactHashChange(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	oldPath := filepath.Join(dir, "old.index.json")
	newPath := filepath.Join(dir, "new.index.json")
	oldIndex, err := writeSnapshotLifecycleIndex(oldPath, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)
	var backupPath string
	for _, artifact := range oldIndex.Bundles[0].Artifacts {
		if artifact.Kind == "backup" {
			backupPath = artifact.Path
			break
		}
	}
	is.NoErr(os.WriteFile(backupPath, []byte("{\"changed\":true}\n"), 0o644))
	_, err = writeSnapshotLifecycleIndex(newPath, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)

	report, err := diffSnapshotLifecycleIndexes(oldPath, newPath)

	is.True(err != nil)
	is.True(!report.OK)
	is.Equal(len(report.ChangedBundles), 1)
	is.Equal(len(report.ChangedBundles[0].ArtifactChanges), 1)
	is.Equal(report.ChangedBundles[0].ArtifactChanges[0].Change, "changed")
}

func TestBuildSnapshotLifecycleIndexPublishReportDryRun(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	out := filepath.Join(dir, "contextdb-backups.index.json")
	_, err := writeSnapshotLifecycleIndex(out, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)

	report, err := buildSnapshotLifecycleIndexPublishReport(context.Background(), nil, out, snapshotLifecycleIndexPublishOptions{
		DryRun: true,
	})

	is.NoErr(err)
	is.True(report.OK)
	is.True(report.DryRun)
	is.True(!report.Published)
	is.Equal(report.Payload.Kind, "contextdb.lifecycle.index")
	is.Equal(report.Payload.TotalBundles, 1)
	is.Equal(report.Payload.Bundles[0].Summary, "prod-20260530T233000.lifecycle.json")
	is.True(report.Payload.Bundles[0].ArtifactCount > 0)
}

func TestBuildSnapshotLifecycleIndexPublishReportExecutesHTTPPublish(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	out := filepath.Join(dir, "contextdb-backups.index.json")
	_, err := writeSnapshotLifecycleIndex(out, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		is.Equal(r.Method, http.MethodPost)
		is.Equal(r.Header.Get("Authorization"), "Bearer index-token")
		var payload snapshotLifecycleIndexPublishPayload
		is.NoErr(json.NewDecoder(r.Body).Decode(&payload))
		is.Equal(payload.Kind, "contextdb.lifecycle.index")
		is.Equal(payload.TotalBundles, 1)
		is.Equal(payload.Bundles[0].Namespace, "prod")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"published":true}`))
	}))
	defer srv.Close()

	report, err := buildSnapshotLifecycleIndexPublishReport(context.Background(), srv.Client(), out, snapshotLifecycleIndexPublishOptions{
		PublishURL: srv.URL,
		Method:     http.MethodPost,
		Token:      "index-token",
		DryRun:     false,
	})

	is.NoErr(err)
	is.True(report.OK)
	is.True(report.Published)
	is.Equal(report.Status, "201 Created")
	is.Equal(report.Response, `{"published":true}`)
}

func TestBuildSnapshotLifecycleIndexPublishReportRequiresPublishURLWhenExecuting(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	out := filepath.Join(dir, "contextdb-backups.index.json")
	_, err := writeSnapshotLifecycleIndex(out, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)

	report, err := buildSnapshotLifecycleIndexPublishReport(context.Background(), nil, out, snapshotLifecycleIndexPublishOptions{
		DryRun: false,
	})

	is.True(err != nil)
	is.True(!report.OK)
	is.True(strings.Contains(report.ValidationErrors[0], "--publish-url"))
}

func TestBuildSnapshotLifecycleIndexPublishDriftReportNoDrift(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	out := filepath.Join(dir, "contextdb-backups.index.json")
	index, err := writeSnapshotLifecycleIndex(out, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)
	payload := buildSnapshotLifecycleIndexPublishPayload(index)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		is.Equal(r.Method, http.MethodGet)
		is.Equal(r.Header.Get("Authorization"), "Bearer index-token")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	report, err := buildSnapshotLifecycleIndexPublishDriftReport(context.Background(), srv.Client(), out, snapshotLifecycleIndexPublishDriftOptions{
		PublishedURL: srv.URL,
		Token:        "index-token",
	})

	is.NoErr(err)
	is.True(report.OK)
	is.True(!report.Drift)
	is.Equal(report.Status, "200 OK")
	is.Equal(len(report.Differences), 0)
	is.Equal(report.LocalPayload.TotalBundles, 1)
	is.Equal(report.PublishedPayload.TotalBundles, 1)
}

func TestBuildSnapshotLifecycleIndexPublishDriftReportFindsDrift(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	out := filepath.Join(dir, "contextdb-backups.index.json")
	index, err := writeSnapshotLifecycleIndex(out, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)
	payload := buildSnapshotLifecycleIndexPublishPayload(index)
	payload.TotalBundles = 2
	payload.Bundles[0].Decision = "prune"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(snapshotLifecycleIndexPublishReport{Payload: payload})
	}))
	defer srv.Close()

	report, err := buildSnapshotLifecycleIndexPublishDriftReport(context.Background(), srv.Client(), out, snapshotLifecycleIndexPublishDriftOptions{
		PublishedURL: srv.URL,
	})

	is.True(err != nil)
	is.True(!report.OK)
	is.True(report.Drift)
	is.True(len(report.Differences) >= 2)
	is.True(strings.Contains(strings.Join(report.Differences, "\n"), "total_bundles differs"))
	is.True(strings.Contains(strings.Join(report.Differences, "\n"), "decision differs"))
}

func TestBuildSnapshotLifecycleIndexPublishFreshnessReportFresh(t *testing.T) {
	is := is.New(t)
	payload := snapshotLifecycleIndexPublishPayload{
		Kind:        "contextdb.lifecycle.index",
		GeneratedAt: "2026-06-01T11:30:00Z",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		is.Equal(r.Method, http.MethodGet)
		is.Equal(r.Header.Get("Authorization"), "Bearer index-token")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	report, err := buildSnapshotLifecycleIndexPublishFreshnessReport(context.Background(), srv.Client(), snapshotLifecycleIndexPublishFreshnessOptions{
		PublishedURL: srv.URL,
		Token:        "index-token",
		MaxAge:       time.Hour,
		Now:          time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})

	is.NoErr(err)
	is.True(report.OK)
	is.True(report.Fresh)
	is.Equal(report.Status, "200 OK")
	is.Equal(report.GeneratedAt, "2026-06-01T11:30:00Z")
	is.Equal(report.AgeSeconds, int64(1800))
	is.Equal(report.MaxAgeSeconds, int64(3600))
}

func TestBuildSnapshotLifecycleIndexPublishFreshnessReportStale(t *testing.T) {
	is := is.New(t)
	payload := snapshotLifecycleIndexPublishPayload{
		Kind:        "contextdb.lifecycle.index",
		GeneratedAt: "2026-06-01T10:30:00Z",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(snapshotLifecycleIndexPublishReport{Payload: payload})
	}))
	defer srv.Close()

	report, err := buildSnapshotLifecycleIndexPublishFreshnessReport(context.Background(), srv.Client(), snapshotLifecycleIndexPublishFreshnessOptions{
		PublishedURL: srv.URL,
		MaxAge:       time.Hour,
		Now:          time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})

	is.True(err != nil)
	is.True(!report.OK)
	is.True(!report.Fresh)
	is.Equal(report.AgeSeconds, int64(5400))
	is.True(strings.Contains(strings.Join(report.ValidationErrors, "\n"), "exceeds max age"))
}

func TestBuildPublishedBackupFreshnessCheck(t *testing.T) {
	is := is.New(t)
	payload := snapshotLifecycleIndexPublishPayload{
		Kind:        "contextdb.lifecycle.index",
		GeneratedAt: "2026-06-01T11:30:00Z",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		is.Equal(r.Header.Get("Authorization"), "Bearer index-token")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	check := buildPublishedBackupFreshnessCheck(context.Background(), srv.Client(), snapshotLifecycleIndexPublishFreshnessOptions{
		PublishedURL: srv.URL,
		Token:        "index-token",
		MaxAge:       time.Hour,
		Now:          time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})

	is.True(check.OK)
	is.Equal(check.Name, "published_backup_freshness")
	is.True(strings.Contains(check.Detail, "age_seconds=1800"))
	is.True(strings.Contains(check.Detail, "max_age_seconds=3600"))
}

func TestBuildPublishedBackupFreshnessCheckReportsStale(t *testing.T) {
	is := is.New(t)
	payload := snapshotLifecycleIndexPublishPayload{
		Kind:        "contextdb.lifecycle.index",
		GeneratedAt: "2026-06-01T10:30:00Z",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(snapshotLifecycleIndexPublishReport{Payload: payload})
	}))
	defer srv.Close()

	check := buildPublishedBackupFreshnessCheck(context.Background(), srv.Client(), snapshotLifecycleIndexPublishFreshnessOptions{
		PublishedURL: srv.URL,
		MaxAge:       time.Hour,
		Now:          time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})

	is.True(!check.OK)
	is.Equal(check.Name, "published_backup_freshness")
	is.True(strings.Contains(check.Detail, "age_seconds=5400"))
	is.True(strings.Contains(check.Detail, "exceeds max age"))
}

func TestBuildPublishedBackupDriftCheck(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	out := filepath.Join(dir, "contextdb-backups.index.json")
	index, err := writeSnapshotLifecycleIndex(out, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)
	payload := buildSnapshotLifecycleIndexPublishPayload(index)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		is.Equal(r.Header.Get("Authorization"), "Bearer index-token")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	check := buildPublishedBackupDriftCheck(context.Background(), srv.Client(), out, snapshotLifecycleIndexPublishDriftOptions{
		PublishedURL: srv.URL,
		Token:        "index-token",
	})

	is.True(check.OK)
	is.Equal(check.Name, "published_backup_drift")
	is.True(strings.Contains(check.Detail, "drift=false"))
	is.True(strings.Contains(check.Detail, "differences=0"))
}

func TestBuildPublishedBackupDriftCheckReportsDrift(t *testing.T) {
	is := is.New(t)
	dir := t.TempDir()
	writeLifecycleFixture(t, dir, "prod", "2026-05-30T23:30:00Z", false)
	out := filepath.Join(dir, "contextdb-backups.index.json")
	index, err := writeSnapshotLifecycleIndex(out, snapshotLifecycleIndexOptions{
		Dir:       dir,
		Namespace: "prod",
		Keep:      1,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)
	payload := buildSnapshotLifecycleIndexPublishPayload(index)
	payload.TotalBundles = 2
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(snapshotLifecycleIndexPublishReport{Payload: payload})
	}))
	defer srv.Close()

	check := buildPublishedBackupDriftCheck(context.Background(), srv.Client(), out, snapshotLifecycleIndexPublishDriftOptions{
		PublishedURL: srv.URL,
	})

	is.True(!check.OK)
	is.Equal(check.Name, "published_backup_drift")
	is.True(strings.Contains(check.Detail, "drift=true"))
	is.True(strings.Contains(check.Detail, "total_bundles differs"))
}

func TestBuildRankingEvalSnapshotReport(t *testing.T) {
	is := is.New(t)

	report, err := buildRankingEvalSnapshotReport(context.Background(), rankingEvalSnapshotOptions{
		TopK:        5,
		GeneratedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})

	is.NoErr(err)
	is.Equal(report.SchemaVersion, 1)
	is.Equal(report.GeneratedAt, "2026-06-01T12:00:00Z")
	is.Equal(report.Corpus, "representative")
	is.Equal(report.TopK, 5)
	is.True(report.TotalQueries > 0)
	is.Equal(report.PassedQueries, report.TotalQueries)
	is.Equal(report.FailedQueries, 0)
	is.True(report.MeanReciprocal > 0)
	is.Equal(len(report.Queries), report.TotalQueries)
	is.True(len(report.Queries[0].TopResults) > 0)
	is.True(report.Queries[0].TopResults[0].Score > 0)
}

func TestBuildRankingEvalMarkdown(t *testing.T) {
	is := is.New(t)

	report, err := buildRankingEvalSnapshotReport(context.Background(), rankingEvalSnapshotOptions{
		TopK:        5,
		GeneratedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})

	is.NoErr(err)
	markdown := buildRankingEvalMarkdown(report)
	is.True(strings.Contains(markdown, "# Ranking Eval Recap"))
	is.True(strings.Contains(markdown, "Mean reciprocal rank"))
	is.True(strings.Contains(markdown, "representative"))
	is.True(strings.Contains(markdown, "## Query Results"))
	is.True(strings.Contains(markdown, report.Queries[0].ID))
	is.True(strings.Contains(markdown, "yes"))
	is.True(strings.Contains(markdown, "sim "))
}

func TestBuildRankingEvalDiffReport(t *testing.T) {
	is := is.New(t)

	current, err := buildRankingEvalSnapshotReport(context.Background(), rankingEvalSnapshotOptions{
		TopK:        5,
		GeneratedAt: time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
	})
	is.NoErr(err)
	previous := copyRankingEvalSnapshotReport(current)
	previous.GeneratedAt = "2026-06-01T12:00:00Z"
	previous.MeanReciprocal -= 0.1
	previous.PassedQueries--
	previous.Queries[0].Passed = false
	previous.Queries[0].CorrectRank = 3
	previous.Queries[0].ReciprocalRank = 1.0 / 3.0
	previous.Queries[0].TopResults[0].Score -= 0.2

	diff := buildRankingEvalDiffReport(previous, current)

	is.Equal(diff.SchemaVersion, 1)
	is.Equal(diff.ComparedQueries, current.TotalQueries)
	is.True(diff.MRRDelta > 0.099 && diff.MRRDelta < 0.101)
	is.Equal(diff.PassedDelta, 1)
	is.Equal(len(diff.PassChangedQueries), 1)
	is.Equal(diff.PassChangedQueries[0], current.Queries[0].ID)
	is.Equal(diff.LargestRankMovements[0].ID, current.Queries[0].ID)
	is.Equal(diff.LargestRankMovements[0].RankDelta, 2)
	is.True(diff.LargestScoreMovements[0].TopScoreDelta > 0)

	markdown := buildRankingEvalDiffMarkdown(diff)
	is.True(strings.Contains(markdown, "# Ranking Eval Diff"))
	is.True(strings.Contains(markdown, "Largest Rank Movements"))
	is.True(strings.Contains(markdown, current.Queries[0].ID))
	is.True(strings.Contains(markdown, "+0.200"))
}

func TestRankingEvalBaselineArtifactPaths(t *testing.T) {
	is := is.New(t)

	dir := filepath.Join(t.TempDir(), "ranking-baselines")
	paths, err := rankingEvalBaselineArtifactPaths(dir, "0.61.0")

	is.NoErr(err)
	is.Equal(paths.Version, "v0.61.0")
	is.Equal(paths.Dir, dir)
	is.Equal(paths.JSONPath, filepath.Join(dir, "ranking-eval-v0.61.0.json"))
	is.Equal(paths.MarkdownPath, filepath.Join(dir, "ranking-eval-v0.61.0.md"))
}

func TestResolveRankingEvalBaselineComparePath(t *testing.T) {
	is := is.New(t)

	dir := t.TempDir()
	for _, name := range []string{
		"ranking-eval-v0.59.0.json",
		"ranking-eval-v0.60.0.json",
		"ranking-eval-v0.61.0.json",
		"ranking-eval-v0.60.0.md",
		"ranking-eval-not-semver.json",
	} {
		is.NoErr(os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o644))
	}

	path, err := resolveRankingEvalBaselineComparePath(dir, "v0.61.0")

	is.NoErr(err)
	is.Equal(path, filepath.Join(dir, "ranking-eval-v0.60.0.json"))
}

func TestResolveRankingEvalBaselineComparePathRequiresPrevious(t *testing.T) {
	is := is.New(t)

	dir := t.TempDir()
	is.NoErr(os.WriteFile(filepath.Join(dir, "ranking-eval-v0.61.0.json"), []byte("{}\n"), 0o644))

	_, err := resolveRankingEvalBaselineComparePath(dir, "v0.61.0")

	is.True(err != nil)
	is.True(strings.Contains(err.Error(), "no previous ranking eval baseline"))
}

func copyRankingEvalSnapshotReport(report rankingEvalSnapshotReport) rankingEvalSnapshotReport {
	report.Queries = append([]rankingEvalSnapshotQuery(nil), report.Queries...)
	for i := range report.Queries {
		report.Queries[i].TopResults = append([]rankingEvalSnapshotResult(nil), report.Queries[i].TopResults...)
	}
	return report
}

func TestBuildStoreConsistencyCheck(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	kv := memstore.NewKVStore()
	nodeID := uuid.New()
	node := core.Node{
		ID:          nodeID,
		Namespace:   "prod",
		Properties:  map[string]any{"text": "store consistency probe"},
		Vector:      []float32{1, 0, 0, 0},
		Fingerprint: core.ContentFingerprint("store consistency probe"),
		Confidence:  0.9,
		ValidFrom:   time.Now().Add(-time.Minute),
		TxTime:      time.Now().Add(-time.Minute),
	}
	is.NoErr(graph.UpsertNode(ctx, node))
	vecs.RegisterNode(node)
	is.NoErr(vecs.Index(ctx, core.VectorEntry{Namespace: "prod", NodeID: &nodeID, Vector: node.Vector}))

	check := buildStoreConsistencyCheck(ctx, graph, vecs, kv, "prod", 10)

	is.True(check.OK)
	is.Equal(check.Name, "store_consistency")
	is.True(strings.Contains(check.Detail, "sampled=1"))
	is.True(strings.Contains(check.Detail, "fingerprints=1"))
	is.True(strings.Contains(check.Detail, "vectors=1"))
}

func TestBuildStoreConsistencyCheckFindsVectorRebuildCandidate(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	kv := memstore.NewKVStore()
	node := core.Node{
		ID:         uuid.New(),
		Namespace:  "prod",
		Properties: map[string]any{"text": "missing vector index"},
		Vector:     []float32{1, 0, 0, 0},
		Confidence: 0.9,
		ValidFrom:  time.Now().Add(-time.Minute),
		TxTime:     time.Now().Add(-time.Minute),
	}
	is.NoErr(graph.UpsertNode(ctx, node))

	check := buildStoreConsistencyCheck(ctx, graph, vecs, kv, "prod", 10)

	is.True(!check.OK)
	is.True(strings.Contains(check.Detail, "vector rebuild candidate"))
}

func TestBuildKVConsistencyCheck(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	kv := memstore.NewKVStore()
	is.NoErr(kv.Set(ctx, "context:prod:hot", []byte("cached"), 0))

	check := buildKVConsistencyCheck(ctx, kv, []string{"context:prod:hot", "context:prod:hot"})

	is.True(check.OK)
	is.Equal(check.Name, "kv_consistency")
	is.True(strings.Contains(check.Detail, "keys=1"))
	is.True(strings.Contains(check.Detail, "present=1"))
	is.True(strings.Contains(check.Detail, "missing=0"))
}

func TestBuildKVConsistencyCheckFindsRefreshCandidate(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	kv := memstore.NewKVStore()

	check := buildKVConsistencyCheck(ctx, kv, []string{"context:prod:missing"})

	is.True(!check.OK)
	is.True(strings.Contains(check.Detail, "refresh_candidates=1"))
	is.True(strings.Contains(check.Detail, "kv refresh candidate"))
}

func TestBuildKVRefreshReportDryRun(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	kv := memstore.NewKVStore()
	is.NoErr(kv.Set(ctx, "context:prod:present", []byte("cached"), 0))

	report, err := buildKVRefreshReport(ctx, kv, kvRefreshOptions{
		Keys:        []string{"context:prod:missing", "context:prod:present", "context:prod:missing"},
		Value:       []byte("refreshed"),
		ValueSource: "literal",
		GeneratedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})

	is.NoErr(err)
	is.True(report.OK)
	is.True(report.DryRun)
	is.Equal(report.GeneratedAt, "2026-06-01T12:00:00Z")
	is.Equal(report.Keys, 2)
	is.Equal(report.Present, 1)
	is.Equal(report.Missing, 1)
	is.Equal(report.RefreshCandidates, 1)
	is.Equal(report.Written, 0)
	is.Equal(report.Skipped, 1)
	is.Equal(report.Items[0].Action, "plan_write")
	is.Equal(report.Items[1].Action, "skip_present")
	value, err := kv.Get(ctx, "context:prod:missing")
	is.NoErr(err)
	is.Equal(len(value), 0)
}

func TestBuildKVRefreshReportExecute(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	kv := memstore.NewKVStore()

	report, err := buildKVRefreshReport(ctx, kv, kvRefreshOptions{
		Keys:       []string{"context:prod:missing"},
		Value:      []byte("refreshed"),
		TTLSeconds: 60,
		Execute:    true,
	})

	is.NoErr(err)
	is.True(report.OK)
	is.True(!report.DryRun)
	is.Equal(report.RefreshCandidates, 1)
	is.Equal(report.Written, 1)
	is.Equal(report.Items[0].Action, "written")
	is.Equal(report.Items[0].TTLSeconds, 60)
	value, err := kv.Get(ctx, "context:prod:missing")
	is.NoErr(err)
	is.Equal(string(value), "refreshed")
}

func TestBuildKVRefreshReportOverwrite(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	kv := memstore.NewKVStore()
	is.NoErr(kv.Set(ctx, "context:prod:present", []byte("cached"), 0))

	report, err := buildKVRefreshReport(ctx, kv, kvRefreshOptions{
		Keys:      []string{"context:prod:present"},
		Value:     []byte("refreshed"),
		Overwrite: true,
		Execute:   true,
	})

	is.NoErr(err)
	is.True(report.OK)
	is.Equal(report.Present, 1)
	is.Equal(report.RefreshCandidates, 1)
	is.Equal(report.Written, 1)
	value, err := kv.Get(ctx, "context:prod:present")
	is.NoErr(err)
	is.Equal(string(value), "refreshed")
}

func TestBuildKVRefreshReportRequiresValue(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	kv := memstore.NewKVStore()

	report, err := buildKVRefreshReport(ctx, kv, kvRefreshOptions{
		Keys: []string{"context:prod:missing"},
	})

	is.True(err != nil)
	is.True(!report.OK)
	is.True(strings.Contains(strings.Join(report.ValidationErrors, "\n"), "refresh value"))
}

func TestKVRefreshValueDerivesRecentNodes(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	olderID := uuid.New()
	newerID := uuid.New()
	is.NoErr(graph.UpsertNode(ctx, core.Node{
		ID:         olderID,
		Namespace:  "prod",
		Labels:     []string{"SessionContext"},
		Properties: map[string]any{"content": "older deploy note", "source": "standup"},
		ValidFrom:  time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC),
		TxTime:     time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC),
		Confidence: 0.8,
	}))
	is.NoErr(graph.UpsertNode(ctx, core.Node{
		ID:            newerID,
		Namespace:     "prod",
		Labels:        []string{"SessionContext"},
		Properties:    map[string]any{"content": "newer deploy note", "priority": "high"},
		ValidFrom:     time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		TxTime:        time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		Confidence:    0.9,
		EpistemicType: core.EpistemicObservation,
	}))

	data, source, err := kvRefreshValue(ctx, graph, kvRefreshValueOptions{
		Derive:          "recent-nodes",
		DeriveNamespace: "prod",
		DeriveLabels:    []string{"SessionContext"},
		DeriveLimit:     1,
	})

	is.NoErr(err)
	is.Equal(source, "derived:recent-nodes")
	var value kvRefreshRecentNodesValue
	is.NoErr(json.Unmarshal(data, &value))
	is.Equal(value.Kind, "contextdb.kv.derived.recent_nodes.v1")
	is.Equal(value.Namespace, "prod")
	is.Equal(value.Count, 1)
	is.Equal(value.Nodes[0].ID, newerID.String())
	is.Equal(value.Nodes[0].Text, "newer deploy note")
	is.Equal(value.Nodes[0].EpistemicType, core.EpistemicObservation)
	is.Equal(value.Nodes[0].Properties["priority"], "high")
}

func TestKVRefreshValueRejectsMixedSources(t *testing.T) {
	is := is.New(t)

	_, _, err := kvRefreshValue(context.Background(), memstore.NewGraphStore(), kvRefreshValueOptions{
		Value:  "literal",
		Derive: "recent-nodes",
	})

	is.True(err != nil)
	is.True(strings.Contains(err.Error(), "mutually exclusive"))
}

func TestBuildVectorIndexRepairReportDryRun(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	node := core.Node{
		ID:         uuid.New(),
		Namespace:  "prod",
		Properties: map[string]any{"text": "dry run repair candidate"},
		Vector:     []float32{1, 0, 0, 0},
		Confidence: 0.9,
		ValidFrom:  time.Now().Add(-time.Minute),
		TxTime:     time.Now().Add(-time.Minute),
	}
	is.NoErr(graph.UpsertNode(ctx, node))

	report, err := buildVectorIndexRepairReport(ctx, graph, vecs, "prod", 10, false)

	is.NoErr(err)
	is.True(report.OK)
	is.True(report.DryRun)
	is.Equal(report.SampledNodes, 1)
	is.Equal(report.VectorNodes, 1)
	is.Equal(report.CandidateIDs, []string{node.ID.String()})
	is.Equal(len(report.ReindexedIDs), 0)

	check := buildStoreConsistencyCheck(ctx, graph, vecs, memstore.NewKVStore(), "prod", 10)
	is.True(!check.OK)
}

func TestBuildVectorIndexRepairReportExecute(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()
	graph := memstore.NewGraphStore()
	vecs := memstore.NewVectorIndex()
	node := core.Node{
		ID:         uuid.New(),
		Namespace:  "prod",
		Properties: map[string]any{"text": "execute repair candidate"},
		Vector:     []float32{1, 0, 0, 0},
		Confidence: 0.9,
		ValidFrom:  time.Now().Add(-time.Minute),
		TxTime:     time.Now().Add(-time.Minute),
	}
	is.NoErr(graph.UpsertNode(ctx, node))

	report, err := buildVectorIndexRepairReport(ctx, graph, vecs, "prod", 10, true)

	is.NoErr(err)
	is.True(report.OK)
	is.True(!report.DryRun)
	is.Equal(report.CandidateIDs, []string{node.ID.String()})
	is.Equal(report.ReindexedIDs, []string{node.ID.String()})

	check := buildStoreConsistencyCheck(ctx, graph, vecs, memstore.NewKVStore(), "prod", 10)
	is.True(check.OK)
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

func TestBuildNornPublishReportDryRun(t *testing.T) {
	is := is.New(t)

	entry, err := buildNornManifestEntry(nornManifestOptions{
		App:         "contextdb",
		Name:        "contextdb",
		Endpoint:    "https://contextdb.example.test",
		GRPCAddr:    ":7700",
		RESTAddr:    ":7701",
		ObserveAddr: ":7702",
		Tags:        []string{"contextdb", "rest", "graphql"},
	})
	is.NoErr(err)

	report, err := buildNornPublishReport(context.Background(), nil, entry, nornPublishOptions{
		DryRun: true,
	})

	is.NoErr(err)
	is.True(report.OK)
	is.True(report.DryRun)
	is.True(!report.Published)
	is.Equal(report.Entry.Endpoint, "https://contextdb.example.test")
}

func TestBuildNornPublishReportExecutesHTTPPublish(t *testing.T) {
	is := is.New(t)

	entry, err := buildNornManifestEntry(nornManifestOptions{
		App:         "contextdb",
		Name:        "contextdb-mini",
		Endpoint:    "https://contextdb.example.test",
		GRPCAddr:    ":7700",
		RESTAddr:    ":7701",
		ObserveAddr: ":7702",
		Tags:        []string{"contextdb", "rest", "graphql"},
	})
	is.NoErr(err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		is.Equal(r.Method, http.MethodPut)
		is.Equal(r.Header.Get("Authorization"), "Bearer test-token")
		is.Equal(r.Header.Get("Content-Type"), "application/json")
		var posted nornManifestEntry
		is.NoErr(json.NewDecoder(r.Body).Decode(&posted))
		is.Equal(posted.App, "contextdb")
		is.Equal(posted.Name, "contextdb-mini")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	report, err := buildNornPublishReport(context.Background(), srv.Client(), entry, nornPublishOptions{
		PublishURL: srv.URL,
		Method:     http.MethodPut,
		Token:      "test-token",
		DryRun:     false,
	})

	is.NoErr(err)
	is.True(report.OK)
	is.True(report.Published)
	is.Equal(report.Status, "202 Accepted")
	is.Equal(report.Response, `{"ok":true}`)
}

func TestBuildNornPublishReportRequiresPublishURLWhenExecuting(t *testing.T) {
	is := is.New(t)

	entry, err := buildNornManifestEntry(nornManifestOptions{
		App:         "contextdb",
		Name:        "contextdb",
		Endpoint:    "https://contextdb.example.test",
		GRPCAddr:    ":7700",
		RESTAddr:    ":7701",
		ObserveAddr: ":7702",
		Tags:        []string{"contextdb", "rest", "graphql"},
	})
	is.NoErr(err)

	report, err := buildNornPublishReport(context.Background(), nil, entry, nornPublishOptions{
		DryRun: false,
	})

	is.True(err != nil)
	is.True(!report.OK)
	is.True(strings.Contains(report.ValidationErrors[0], "--publish-url"))
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
