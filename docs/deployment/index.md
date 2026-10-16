---
title: Deployment
---

# Deployment

Run contextdb embedded, in Docker, on Kubernetes, or on the internal Mini/Norn deployment. Scaled mode is a future design and is unavailable in the current build.

| Deployment | Best for |
|:-----------|:---------|
| [Embedded](embedded) | Local development, sidecars, CLIs, tests |
| [Docker](docker) | Single-node server with simple persistence |
| [Operations](operations) | Live health checks and operational commands |
| [Backup Runbook](backup-runbook) | Scheduled snapshot export, restore preview, backup marker, doctor verification, and published catalog repair closure |
| [Mini/Norn](norn) | Internal live instance on Aaron's Mac mini |
| [Kubernetes](kubernetes) | Container orchestration and ingress |
| [Scaled design](scaled) | Future Qdrant + Redis + Postgres topology; unavailable in the current build |
