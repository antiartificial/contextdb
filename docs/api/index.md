---
title: API Reference
---

# API Reference

contextdb exposes seven interfaces plus a small set of published JSON contracts:

- [**Go SDK**](go-sdk): the primary embedded client (`pkg/client`)
- [**gRPC API**](grpc): JSON-over-gRPC on port 7700
- [**REST API**](rest): HTTP/JSON on port 7701
- [**GraphQL API**](graphql): graph-shaped search and inspection on `/graphql`
- [**Python SDK**](python-sdk): REST client for Python applications
- [**TypeScript SDK**](typescript-sdk): REST client for TypeScript/Node.js applications
- [**Query DSL**](dsl): Pipe syntax and CQL query languages
- [**Published Schemas**](schemas): machine-readable docs contracts for dashboards and automation

The Go SDK supports all four deployment modes (embedded, standard, remote, scaled). The Python and TypeScript SDKs connect to a running contextdb server via the REST API. The Query DSL provides two syntax tiers (pipe and CQL) that compile to the same AST. Published schemas live under `/schemas/` on the docs site so integrations can discover stable response metadata contracts.
