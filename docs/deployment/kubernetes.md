---
title: Kubernetes
parent: Deployment
nav_order: 3
---

# Kubernetes (Helm)

contextdb ships a Helm chart for Kubernetes deployment.

## Install

```bash
helm install contextdb deploy/helm/contextdb
```

## Default configuration

The chart deploys contextdb in embedded mode with BadgerDB persistence:

```yaml
# values.yaml defaults
replicaCount: 1

image:
  repository: ghcr.io/antiartificial/contextdb
  tag: "latest"
  pullPolicy: IfNotPresent

service:
  type: ClusterIP
  grpcPort: 7700
  restPort: 7701
  observePort: 7702

resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi

config:
  mode: "embedded"
  dataDir: "/data"
  logLevel: "info"

persistence:
  enabled: true
  size: 1Gi
  storageClass: ""
```

## With Postgres

```yaml
# values-postgres.yaml
config:
  mode: "standard"

postgres:
  enabled: true
  dsn: "postgres://user:pass@postgres-host:5432/contextdb?sslmode=require"

persistence:
  enabled: false  # not needed with Postgres
```

```bash
helm install contextdb deploy/helm/contextdb -f values-postgres.yaml
```

## Autoscaling

```yaml
autoscaling:
  enabled: true
  minReplicas: 1
  maxReplicas: 3
  targetCPUUtilization: 80
```

When autoscaling is enabled, the chart creates a HorizontalPodAutoscaler. Note that only Postgres mode supports multiple replicas -- embedded mode uses local storage.

## Ingress

```yaml
ingress:
  enabled: true
  className: "nginx"
  annotations:
    nginx.ingress.kubernetes.io/backend-protocol: "GRPC"
  hosts:
    - host: contextdb.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: contextdb-tls
      hosts:
        - contextdb.example.com
```

## Exposed ports

| Port | Protocol | Service name |
|:-----|:---------|:-------------|
| 7700 | gRPC | `contextdb-grpc` |
| 7701 | HTTP | `contextdb-rest` |
| 7702 | HTTP | `contextdb-observe` |

## Template rendering

Preview the generated manifests:

```bash
helm template contextdb deploy/helm/contextdb
```

## Lint

```bash
helm lint deploy/helm/contextdb
```

## values.yaml reference

| Key | Type | Default | Description |
|:----|:-----|:--------|:------------|
| `replicaCount` | int | `1` | Number of replicas |
| `image.repository` | string | `ghcr.io/antiartificial/contextdb` | Container image |
| `image.tag` | string | `latest` | Image tag |
| `image.pullPolicy` | string | `IfNotPresent` | Pull policy |
| `service.type` | string | `ClusterIP` | Service type |
| `service.grpcPort` | int | `7700` | gRPC port |
| `service.restPort` | int | `7701` | REST port |
| `service.observePort` | int | `7702` | Observe port |
| `resources.requests.cpu` | string | `100m` | CPU request |
| `resources.requests.memory` | string | `128Mi` | Memory request |
| `resources.limits.cpu` | string | `500m` | CPU limit |
| `resources.limits.memory` | string | `512Mi` | Memory limit |
| `config.mode` | string | `embedded` | Storage mode |
| `config.dataDir` | string | `/data` | BadgerDB directory |
| `config.logLevel` | string | `info` | Log level |
| `persistence.enabled` | bool | `true` | Enable PVC |
| `persistence.size` | string | `1Gi` | PVC size |
| `persistence.storageClass` | string | `""` | Storage class |
| `postgres.enabled` | bool | `false` | Enable Postgres |
| `postgres.dsn` | string | `""` | Postgres DSN |
| `autoscaling.enabled` | bool | `false` | Enable HPA |
| `autoscaling.minReplicas` | int | `1` | Min replicas |
| `autoscaling.maxReplicas` | int | `3` | Max replicas |
| `autoscaling.targetCPUUtilization` | int | `80` | CPU target % |
| `ingress.enabled` | bool | `false` | Enable Ingress |
