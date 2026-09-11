---
title: Access Control
---

# Access Control

Set `CONTEXTDB_AUTH_TOKENS` to a JSON array of full `tenant:permissions:secret` token strings to enable authentication. Tokens are exact configured values; syntactically similar tokens are rejected. Permissions are `read`, `write`, and `admin` (admin is global).

When enabled, REST, gRPC, observability, and admin requests require a token. `/health` remains public for Kubernetes liveness checks. GraphQL and low-level remote-store gRPC methods require `admin`, because they can select arbitrary namespaces. Do not expose a deployment without token configuration to an untrusted network: no configured registry preserves the local unauthenticated compatibility mode.

`write` includes review-cycle execution, acquisition approval/rejection, and recovery within the token's own tenant. `admin` is reserved for global inspection and low-level operations. REST and gRPC high-level requests derive tenant identity from the authenticated token; a conflicting tenant header is rejected or ignored in favor of that authenticated identity.

Use bearer tokens with the SDKs:

```typescript
const db = new ContextDB(url, { token: process.env.CONTEXTDB_TOKEN });
```

```python
with ContextDB(url, token=os.environ["CONTEXTDB_TOKEN"]) as db:
    runs = db.namespace("production").review_worker_runs()
```

The CLI worker reads `CONTEXTDB_TOKEN` or accepts `--token`. Prefer the environment variable to keep the token out of shell history. In registry-enabled deployments, browser admin access requires an authenticated gateway that supplies the bearer header; this change does not add a browser login or token-storage UI. Health probes can use the public observe `/health` endpoint. The default Compose doctor probe is for its default trusted local configuration; adapt the probe or gateway when enabling authentication.
