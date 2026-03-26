"""contextdb Python client wrapping the REST API."""

from __future__ import annotations

from typing import Any

import httpx

from contextdb.types import (
    IngestResult,
    Result,
    RetrieveRequest,
    ScoreParams,
    WriteRequest,
    WriteResult,
)


class ContextDB:
    """Top-level client for a contextdb server.

    Usage::

        db = ContextDB("http://localhost:7701")
        ns = db.namespace("my-app", mode="general")

        result = ns.write(content="Go is fast", source_id="crawler")
        results = ns.retrieve(text="What is Go?", top_k=5)
        db.close()
    """

    def __init__(self, base_url: str, *, timeout: float = 30.0) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = httpx.Client(base_url=self._base_url, timeout=timeout)

    def namespace(self, name: str, mode: str = "general") -> Namespace:
        """Return a namespace handle."""
        return Namespace(self._client, name, mode)

    def ping(self) -> dict[str, Any]:
        """Health check."""
        resp = self._client.get("/v1/ping")
        resp.raise_for_status()
        return resp.json()

    def stats(self) -> dict[str, Any]:
        """Get server stats."""
        resp = self._client.get("/v1/stats")
        resp.raise_for_status()
        return resp.json()

    def close(self) -> None:
        """Close the HTTP client."""
        self._client.close()

    def __enter__(self) -> ContextDB:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()


class Namespace:
    """A namespace-scoped handle for reads and writes."""

    def __init__(self, client: httpx.Client, name: str, mode: str) -> None:
        self._client = client
        self._name = name
        self._mode = mode

    def write(
        self,
        content: str = "",
        source_id: str = "",
        labels: list[str] | None = None,
        properties: dict[str, str] | None = None,
        vector: list[float] | None = None,
        model_id: str = "",
        confidence: float = 0.0,
    ) -> WriteResult:
        """Write a node to this namespace."""
        body: dict[str, Any] = {
            "mode": self._mode,
            "content": content,
            "source_id": source_id,
        }
        if labels:
            body["labels"] = labels
        if properties:
            body["properties"] = properties
        if vector is not None:
            body["vector"] = vector
        if model_id:
            body["model_id"] = model_id
        if confidence > 0:
            body["confidence"] = confidence

        resp = self._client.post(
            f"/v1/namespaces/{self._name}/write", json=body
        )
        resp.raise_for_status()
        data = resp.json()
        return WriteResult(
            node_id=data.get("node_id", ""),
            admitted=data.get("admitted", False),
            reason=data.get("reason", ""),
            conflict_ids=data.get("conflict_ids") or [],
        )

    def retrieve(
        self,
        vector: list[float] | None = None,
        vectors: list[list[float]] | None = None,
        text: str = "",
        seed_ids: list[str] | None = None,
        top_k: int = 10,
        labels: list[str] | None = None,
        score_params: ScoreParams | None = None,
    ) -> list[Result]:
        """Retrieve nodes from this namespace."""
        body: dict[str, Any] = {"top_k": top_k}
        if vector is not None:
            body["vector"] = vector
        if vectors is not None:
            body["vectors"] = vectors
        if text:
            body["text"] = text
        if seed_ids:
            body["seed_ids"] = seed_ids
        if labels:
            body["labels"] = labels
        if score_params:
            body["score_params"] = {
                "similarity_weight": score_params.similarity_weight,
                "confidence_weight": score_params.confidence_weight,
                "recency_weight": score_params.recency_weight,
                "utility_weight": score_params.utility_weight,
                "decay_alpha": score_params.decay_alpha,
            }

        resp = self._client.post(
            f"/v1/namespaces/{self._name}/retrieve", json=body
        )
        resp.raise_for_status()
        data = resp.json()

        return [
            Result(
                id=r.get("id", ""),
                namespace=r.get("namespace", ""),
                labels=r.get("labels") or [],
                properties=r.get("properties") or {},
                score=r.get("score", 0.0),
                similarity_score=r.get("similarity_score", 0.0),
                confidence_score=r.get("confidence_score", 0.0),
                recency_score=r.get("recency_score", 0.0),
                utility_score=r.get("utility_score", 0.0),
                retrieval_source=r.get("retrieval_source", ""),
            )
            for r in data.get("results", [])
        ]

    def ingest_text(self, text: str, source_id: str = "") -> IngestResult:
        """Ingest raw text through the extraction pipeline."""
        body = {
            "mode": self._mode,
            "text": text,
            "source_id": source_id,
        }
        resp = self._client.post(
            f"/v1/namespaces/{self._name}/ingest", json=body
        )
        resp.raise_for_status()
        data = resp.json()
        return IngestResult(
            nodes_written=data.get("nodes_written", 0),
            edges_written=data.get("edges_written", 0),
            rejected=data.get("rejected", 0),
        )

    def label_source(self, external_id: str, labels: list[str]) -> None:
        """Set labels on a source."""
        body = {
            "mode": self._mode,
            "external_id": external_id,
            "labels": labels,
        }
        resp = self._client.post(
            f"/v1/namespaces/{self._name}/sources/label", json=body
        )
        resp.raise_for_status()
