---
title: API Reference
nav_order: 6
has_children: true
---

# API Reference

contextdb exposes five interfaces:

- [**Go SDK**](go-sdk) -- the primary embedded client (`pkg/client`)
- [**gRPC API**](grpc) -- JSON-over-gRPC on port 7700
- [**REST API**](rest) -- HTTP/JSON on port 7701
- [**Python SDK**](python-sdk) -- REST client for Python applications
- [**TypeScript SDK**](typescript-sdk) -- REST client for TypeScript/Node.js applications

The Go SDK supports all four deployment modes (embedded, standard, remote, scaled). The Python and TypeScript SDKs connect to a running contextdb server via the REST API.
