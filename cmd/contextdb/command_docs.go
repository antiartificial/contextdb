package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/antiartificial/contextdb/internal/buildinfo"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func runDocs(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb docs: expected schema-catalog")
		os.Exit(2)
	}
	switch args[0] {
	case "schema-catalog":
		runDocsSchemaCatalog(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "contextdb docs: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runDocsSchemaCatalog(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "contextdb docs schema-catalog: expected verify")
		os.Exit(2)
	}
	switch args[0] {
	case "verify":
		runDocsSchemaCatalogVerify(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "contextdb docs schema-catalog: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func runDocsSchemaCatalogVerify(args []string) {
	fs := flag.NewFlagSet("contextdb docs schema-catalog verify", flag.ExitOnError)
	indexPath := fs.String("index", "docs/public/schemas/index.json", "public schema catalog index")
	publicRoot := fs.String("public-root", "docs/public", "docs public artifact root")
	reportOut := fs.Bool("report", false, "print a JSON schema catalog verification report")
	annotationsOut := fs.Bool("annotations", false, "print CI annotation lines for schema catalog drift")
	annotationsOutPath := fs.String("annotations-out", "", "CI annotation lines for schema catalog drift to write")
	_ = fs.Parse(args)

	report, err := verifyPublicSchemaCatalog(*indexPath, *publicRoot)
	if strings.TrimSpace(*annotationsOutPath) != "" {
		if writeErr := writeTextFile(*annotationsOutPath, buildPublicSchemaCatalogFailureAnnotations(report)); writeErr != nil && err == nil {
			err = writeErr
		}
	}
	if *reportOut || err != nil {
		writeIndentedJSON(report)
	}
	if *annotationsOut {
		annotations := buildPublicSchemaCatalogFailureAnnotations(report)
		if strings.TrimSpace(annotations) == "" {
			fmt.Fprintln(os.Stdout)
		} else {
			fmt.Print(annotations)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "contextdb docs schema-catalog verify: %v\n", err)
		os.Exit(1)
	}
	if !*reportOut && !*annotationsOut {
		fmt.Fprintln(os.Stdout, "ok")
	}
}

type publicSchemaCatalog struct {
	SchemaVersion int                        `json:"schema_version"`
	Schemas       []publicSchemaCatalogEntry `json:"schemas"`
}

type publicSchemaCatalogEntry struct {
	ID           string `json:"id"`
	Title        string `json:"title,omitempty"`
	Href         string `json:"href"`
	PublicURL    string `json:"public_url,omitempty"`
	JSONSchemaID string `json:"json_schema_id"`
	Feature      string `json:"feature"`
	Owner        string `json:"owner"`
	IntroducedIn string `json:"introduced_in"`
	CatalogedIn  string `json:"cataloged_in"`
	Status       string `json:"status"`
}

type publicSchemaCatalogVerifyReport struct {
	Kind             string                          `json:"kind"`
	SchemaVersion    int                             `json:"schema_version"`
	ContextDBVersion string                          `json:"contextdb_version"`
	CheckedAt        string                          `json:"checked_at"`
	IndexFile        string                          `json:"index_file"`
	PublicRoot       string                          `json:"public_root"`
	OK               bool                            `json:"ok"`
	Schemas          []publicSchemaCatalogVerifyItem `json:"schemas"`
	ValidationErrors []string                        `json:"validation_errors,omitempty"`
}

type publicSchemaCatalogVerifyItem struct {
	ID               string   `json:"id"`
	Href             string   `json:"href"`
	Path             string   `json:"path"`
	Exists           bool     `json:"exists"`
	Bytes            int64    `json:"bytes,omitempty"`
	ExpectedSchemaID string   `json:"expected_schema_id,omitempty"`
	ActualSchemaID   string   `json:"actual_schema_id,omitempty"`
	ValidationErrors []string `json:"validation_errors,omitempty"`
}

func verifyPublicSchemaCatalog(indexPath, publicRoot string) (publicSchemaCatalogVerifyReport, error) {
	indexPath = strings.TrimSpace(indexPath)
	publicRoot = strings.TrimSpace(publicRoot)
	report := publicSchemaCatalogVerifyReport{
		Kind:             "contextdb.docs.schema_catalog_verify",
		SchemaVersion:    1,
		ContextDBVersion: buildinfo.Version,
		CheckedAt:        time.Now().UTC().Format(time.RFC3339),
		IndexFile:        indexPath,
		PublicRoot:       publicRoot,
	}
	if indexPath == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--index is required")
	}
	if publicRoot == "" {
		report.ValidationErrors = append(report.ValidationErrors, "--public-root is required")
	}
	if len(report.ValidationErrors) > 0 {
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	var catalog publicSchemaCatalog
	if err := readJSONFile(indexPath, &catalog); err != nil {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("read schema catalog: %v", err))
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	if catalog.SchemaVersion != 1 {
		report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("schema_version = %d, want 1", catalog.SchemaVersion))
	}
	if len(catalog.Schemas) == 0 {
		report.ValidationErrors = append(report.ValidationErrors, "schema catalog has no schemas")
	}
	seen := map[string]bool{}
	for _, entry := range catalog.Schemas {
		item := publicSchemaCatalogVerifyItem{
			ID:               entry.ID,
			Href:             entry.Href,
			Path:             publicSchemaCatalogEntryPath(publicRoot, entry.Href),
			ExpectedSchemaID: entry.JSONSchemaID,
		}
		if strings.TrimSpace(entry.ID) == "" {
			item.ValidationErrors = append(item.ValidationErrors, "id is required")
		} else if seen[entry.ID] {
			item.ValidationErrors = append(item.ValidationErrors, "duplicate id")
		}
		seen[entry.ID] = true
		if strings.TrimSpace(entry.Href) == "" {
			item.ValidationErrors = append(item.ValidationErrors, "href is required")
		}
		if strings.TrimSpace(entry.JSONSchemaID) == "" {
			item.ValidationErrors = append(item.ValidationErrors, "json_schema_id is required")
		}
		data, err := os.ReadFile(item.Path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				item.ValidationErrors = append(item.ValidationErrors, "schema artifact missing")
			} else {
				item.ValidationErrors = append(item.ValidationErrors, fmt.Sprintf("read schema artifact: %v", err))
			}
			report.Schemas = append(report.Schemas, item)
			continue
		}
		item.Exists = true
		item.Bytes = int64(len(data))
		var schemaDoc struct {
			ID string `json:"$id"`
		}
		if err := json.Unmarshal(data, &schemaDoc); err != nil {
			item.ValidationErrors = append(item.ValidationErrors, fmt.Sprintf("decode schema artifact: %v", err))
		}
		item.ActualSchemaID = schemaDoc.ID
		if item.ExpectedSchemaID != "" && schemaDoc.ID != item.ExpectedSchemaID {
			item.ValidationErrors = append(item.ValidationErrors, fmt.Sprintf("$id = %q, want %q", schemaDoc.ID, item.ExpectedSchemaID))
		}
		report.Schemas = append(report.Schemas, item)
	}
	for _, item := range report.Schemas {
		for _, validationErr := range item.ValidationErrors {
			report.ValidationErrors = append(report.ValidationErrors, fmt.Sprintf("%s: %s", item.ID, validationErr))
		}
	}
	report.OK = len(report.ValidationErrors) == 0
	if !report.OK {
		return report, errors.New(strings.Join(report.ValidationErrors, "; "))
	}
	return report, nil
}

func publicSchemaCatalogEntryPath(publicRoot, href string) string {
	href = strings.TrimSpace(href)
	href = strings.TrimPrefix(href, "https://antiartificial.github.io/contextdb")
	href = strings.TrimPrefix(href, "/contextdb")
	href = strings.TrimPrefix(href, "/")
	return filepath.Join(publicRoot, filepath.FromSlash(href))
}

func buildPublicSchemaCatalogFailureAnnotations(report publicSchemaCatalogVerifyReport) string {
	if report.OK && len(report.ValidationErrors) == 0 {
		return ""
	}
	var b strings.Builder
	itemFailures := 0
	for _, item := range report.Schemas {
		for _, validationErr := range item.ValidationErrors {
			itemFailures++
			fmt.Fprintf(&b, "::error file=%s,title=%s::%s\n",
				ciAnnotationEscape(item.Path),
				ciAnnotationEscape("Schema catalog verification"),
				ciAnnotationEscape(fmt.Sprintf("%s: %s", item.ID, validationErr)))
		}
	}
	if itemFailures == 0 {
		for _, validationErr := range report.ValidationErrors {
			fmt.Fprintf(&b, "::error file=%s,title=%s::%s\n",
				ciAnnotationEscape(report.IndexFile),
				ciAnnotationEscape("Schema catalog verification"),
				ciAnnotationEscape(validationErr))
		}
	}
	return b.String()
}
