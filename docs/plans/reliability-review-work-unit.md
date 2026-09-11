# Reliability and review work unit

Status: implementation and verification complete; prepared for v0.123.0. Baseline: main at 875b20f (v0.108.0).

One integrated work unit, implemented by Terra agents, coordinated by Astra, and reviewed by Sol. The release packages this completed work as v0.123.0; no production service deployment is claimed.

## Acceptance plan

1. **Release truth:** inspect available branches/tags and local checkouts; distinguish shipped v0.108 from unsupported v0.109–122 recaps. Preserve the latter as unverified historical proposals, remove passed-release claims, and document this work in its own verified recap. Add a release metadata regression check.
2. **Deployment correctness:** standard mode requires a DSN; unsupported scaled mode fails explicitly; expose effective backend identities without credentials. Compose selects its intended backend and probes a real health endpoint. Listener startup fails synchronously and shutdown is bounded.
3. **Recoverable writes:** persist replayable intent before multi-store mutation, use stable identities, expose pending work and explicit reconciliation, and test injected failures/restarts. Document eventual recovery rather than claim cross-store atomicity.
4. **DSL correctness:** carry edge types from the DSL through hybrid traversal and prove excluded relationships cannot contribute graph results.
5. **Bounded refactor:** split the client and CLI by capability while preserving exported signatures and command behavior. Do this after concurrent edits settle.
6. **Ranking investigation:** inspect a ranked result in its exact namespace and keep a validated, bounded local baseline history with graceful storage failures.
7. **Acquisition review:** optional review mode stores durable candidates outside the belief graph; list/approve/reject through public API; only explicit approval admits evidence; retries do not duplicate candidate admission.
8. **Central review worker:** dry-run-first bounded review cycles, conservative rules evaluator and optional webhook evaluator, action allow-list, durable run summaries, server-side execution and CLI scheduling against the server. Do not infer truth from an LLM or claim unspecified provider integrations. Exercise review-only and explicit mutation flows.
9. **Integrated verification:** full Go tests/vet, relevant race and persistence tests, admin/docs build, SDK tests, HTTP end-to-end flow, and independent Sol review. Fix material findings before final report.

## Coordination

Initial parallel lanes: deployment/DSL; recovery; ranking UI. Primary owns release reconciliation, backend configuration, and shared integration. Freed Terra lanes implement acquisition review and the centralized worker with explicitly assigned files. Structural file moves happen only after owners finish. Final review receives the actual combined diff and test evidence.

## Evidence and limitations

Remote inspection found only main and tags through v0.108.0. The June 7 commit added documentation only for v0.109–122. No second ContextDB checkout was found within the searched Desktop/Documents/Codex worktree locations. This does not establish that no unpublished work exists on another machine.

Validation completed: full Go suite/vet; ingest/retrieval/client/server race checks; TypeScript contract tests/build; Python sync/async mocked contracts; admin/docs builds; live Postgres smoke and advisory lease tests; authenticated HTTP acquisition-to-review and worker flow; real gRPC access-control checks; native arm64 container build and doctor sample write/retrieve plus worker CLI smoke. Interactive browser validation passed: ranking result inspection renders the correct retained fixture claim, evidence workbench, and raw audit; saved baselines survive reload and support deletion. Nullable audit-array rendering is covered by a regression test.

Sol reviewed the integrated implementation and re-reviewed the fixes. Corrected findings included idempotency collisions, wrong ranking fixture context, duplicate candidate event IDs under Postgres, authentication interceptor dispatch, stale ANN node metadata, per-key lock retention, and worker failure exit codes.

Operating limits and next steps: indexed event lookups remain deferred; interrupted writes are repairable rather than atomic; conflicting state and ambiguous worker mutations require manual inspection; no provider-specific adapters or Norn/Hermes deployment was performed. Namespace write tokens intentionally permit namespace review/recovery operations; global admin/GraphQL/raw-store surfaces require admin. Embedded memory has no restart durability. Release metadata identifies this work as v0.123.0.


## Completion and next steps

All eight implementation areas in this work unit are complete. Integrated validation and independent Sol review are complete. Temporary server processes and Docker smoke containers are cleaned up after verification; no production service was deployed.

The next work unit should prioritize indexed pending/idempotency lookup and an operator UI for acquisition candidates, worker runs, assigned failures, and recovery. Expand Postgres crash/fault and long-soak evidence before adding automatic schedules or provider-specific evaluators. The [v0.123.0 recap](../releases/v0.123.0) is the release-review entry point.
