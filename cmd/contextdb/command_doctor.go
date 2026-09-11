package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/antiartificial/contextdb/internal/doctor"
	"net/http"
	"os"
	"strings"
	"time"
)

func kvRefreshReceiptDoctorCommand(report kvRefreshReport) string {
	command := "contextdb doctor"
	seen := map[string]struct{}{}
	for _, item := range report.Items {
		if item.Action != "written" {
			continue
		}
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		command += " --kv-derived-key " + shellQuote(key)
	}
	if len(seen) == 0 {
		return ""
	}
	command += " --report"
	return command
}

func runDoctor(args []string) {
	fs := flag.NewFlagSet("contextdb doctor", flag.ExitOnError)
	baseURL := fs.String("url", getenv("CONTEXTDB_REST_URL", "http://127.0.0.1:7701"), "contextdb REST base URL")
	sampleWrite := fs.Bool("sample-write", false, "write and retrieve a sample probe node")
	sampleNamespace := fs.String("sample-namespace", "_doctor", "namespace to use with --sample-write")
	backupMarker := fs.String("backup-marker", "", "path to a backup marker file to check for recency")
	maxBackupAge := fs.Duration("max-backup-age", 24*time.Hour, "maximum acceptable age for --backup-marker")
	publishedBackupURL := fs.String("published-backup-url", os.Getenv("CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_URL"), "published backup index metadata URL to check for freshness")
	publishedBackupIndex := fs.String("published-backup-index", "", "local lifecycle index path to compare against published backup metadata")
	publishedBackupReceipt := fs.String("published-backup-receipt", "", "published backup repair receipt path to verify against --published-backup-index")
	publishedBackupMethod := fs.String("published-backup-method", getenv("CONTEXTDB_LIFECYCLE_INDEX_PUBLISHED_METHOD", http.MethodGet), "HTTP method for fetching published backup metadata")
	publishedBackupToken := fs.String("published-backup-token", os.Getenv("NORN_TOKEN"), "optional bearer token for the published backup metadata endpoint")
	maxPublishedBackupAge := fs.Duration("max-published-backup-age", 24*time.Hour, "maximum acceptable age for --published-backup-url")
	publishedBackupTimeout := fs.Duration("published-backup-timeout", 5*time.Second, "published backup metadata request timeout")
	storeConsistency := fs.Bool("store-consistency", false, "check local graph, vector, and fingerprint consistency")
	storeNamespace := fs.String("store-namespace", "default", "namespace to check with --store-consistency")
	storeSample := fs.Int("store-sample", 100, "maximum valid graph nodes to sample with --store-consistency")
	var kvKeys repeatedStringFlag
	fs.Var(&kvKeys, "kv-key", "expected KV hot key to check; repeat for multiple keys")
	var kvDerivedKeys repeatedStringFlag
	fs.Var(&kvDerivedKeys, "kv-derived-key", "expected derived KV hot key to check for generated_at freshness; repeat for multiple keys")
	maxKVDerivedAge := fs.Duration("max-kv-derived-age", 24*time.Hour, "maximum acceptable generated_at age for --kv-derived-key")
	kvRefreshReceipt := fs.String("kv-refresh-receipt", "", "derived KV refresh receipt path to verify")
	kvRefreshValueFile := fs.String("kv-refresh-value-file", "", "optional reviewed derived KV value file to compare against --kv-refresh-receipt")
	_ = fs.Parse(args)

	report, err := doctor.Run(context.Background(), doctor.Options{
		BaseURL:         *baseURL,
		SampleWrite:     *sampleWrite,
		SampleNamespace: *sampleNamespace,
		BackupMarker:    *backupMarker,
		MaxBackupAge:    *maxBackupAge,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb doctor: %v\n", err)
		os.Exit(2)
	}
	if strings.TrimSpace(*publishedBackupURL) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), *publishedBackupTimeout)
		defer cancel()
		report.Checks = append(report.Checks, buildPublishedBackupFreshnessCheck(ctx, http.DefaultClient, snapshotLifecycleIndexPublishFreshnessOptions{
			PublishedURL: *publishedBackupURL,
			Method:       *publishedBackupMethod,
			Token:        *publishedBackupToken,
			MaxAge:       *maxPublishedBackupAge,
			Now:          time.Now(),
		}))
		recomputeDoctorReportOK(&report)
	}
	if strings.TrimSpace(*publishedBackupIndex) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), *publishedBackupTimeout)
		defer cancel()
		report.Checks = append(report.Checks, buildPublishedBackupDriftCheck(ctx, http.DefaultClient, *publishedBackupIndex, snapshotLifecycleIndexPublishDriftOptions{
			PublishedURL: *publishedBackupURL,
			Method:       *publishedBackupMethod,
			Token:        *publishedBackupToken,
		}))
		recomputeDoctorReportOK(&report)
	}
	if strings.TrimSpace(*publishedBackupReceipt) != "" {
		report.Checks = append(report.Checks, buildPublishedBackupReceiptVerifyCheck(*publishedBackupReceipt, *publishedBackupIndex))
		recomputeDoctorReportOK(&report)
	}
	if strings.TrimSpace(*kvRefreshReceipt) != "" {
		report.Checks = append(report.Checks, buildKVRefreshReceiptVerifyCheck(*kvRefreshReceipt, *kvRefreshValueFile))
		recomputeDoctorReportOK(&report)
	}
	if *storeConsistency || len(kvKeys) > 0 || len(kvDerivedKeys) > 0 {
		db := openSnapshotDB()
		defer db.Close()
		graph, vecs, kv, _ := db.Stores()
		if *storeConsistency {
			report.Checks = append(report.Checks, buildStoreConsistencyCheck(context.Background(), graph, vecs, kv, *storeNamespace, *storeSample))
		}
		if len(kvKeys) > 0 {
			report.Checks = append(report.Checks, buildKVConsistencyCheck(context.Background(), kv, kvKeys))
		}
		if len(kvDerivedKeys) > 0 {
			report.Checks = append(report.Checks, buildKVDerivedFreshnessCheck(context.Background(), kv, kvDerivedKeys, *maxKVDerivedAge, time.Now()))
		}
		recomputeDoctorReportOK(&report)
	}
	writeIndentedJSON(report)
	if !report.OK {
		os.Exit(1)
	}
}

func recomputeDoctorReportOK(report *doctor.Report) {
	report.OK = true
	for _, check := range report.Checks {
		if !check.OK {
			report.OK = false
			return
		}
	}
}
