"""contextdb async Python client wrapping the REST API.

Mirror of client.py using httpx.AsyncClient for use in async contexts
(Hermes gateway, Ollama-backed tool dispatch, etc.).
"""

from __future__ import annotations

from typing import Any

import httpx

from contextdb.types import (
    AcquisitionConnector,
    IngestResult,
    Result,
    RetrieveRequest,
    ScoreParams,
    WriteRequest,
    WriteResult,
)


class AsyncContextDB:
    """Async client for a contextdb server.

    Usage::

        async with AsyncContextDB("http://localhost:7701") as db:
            ns = db.namespace("my-app", mode="general")
            result = await ns.write(content="Go is fast", source_id="crawler")
            results = await ns.retrieve(text="What is Go?", top_k=5)
    """

    def __init__(self, base_url: str, *, timeout: float = 30.0) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = httpx.AsyncClient(base_url=self._base_url, timeout=timeout)

    def namespace(self, name: str, mode: str = "general") -> AsyncNamespace:
        """Return an async namespace handle."""
        return AsyncNamespace(self._client, name, mode)

    async def ping(self) -> dict[str, Any]:
        resp = await self._client.get("/v1/ping")
        resp.raise_for_status()
        return resp.json()

    async def stats(self) -> dict[str, Any]:
        resp = await self._client.get("/v1/stats")
        resp.raise_for_status()
        return resp.json()

    async def close(self) -> None:
        await self._client.aclose()

    async def __aenter__(self) -> AsyncContextDB:
        return self

    async def __aexit__(self, *args: Any) -> None:
        await self.close()


class AsyncNamespace:
    """Async namespace-scoped handle for reads and writes."""

    def __init__(self, client: httpx.AsyncClient, name: str, mode: str) -> None:
        self._client = client
        self._name = name
        self._mode = mode

    async def write(
        self,
        content: str = "",
        source_id: str = "",
        labels: list[str] | None = None,
        properties: dict[str, str] | None = None,
        vector: list[float] | None = None,
        model_id: str = "",
        confidence: float = 0.0,
    ) -> WriteResult:
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

        resp = await self._client.post(
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

    async def retrieve(
        self,
        vector: list[float] | None = None,
        vectors: list[list[float]] | None = None,
        text: str = "",
        seed_ids: list[str] | None = None,
        top_k: int = 10,
        labels: list[str] | None = None,
        score_params: ScoreParams | None = None,
    ) -> list[Result]:
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

        resp = await self._client.post(
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

    async def ingest_text(self, text: str, source_id: str = "") -> IngestResult:
        body = {
            "mode": self._mode,
            "text": text,
            "source_id": source_id,
        }
        resp = await self._client.post(
            f"/v1/namespaces/{self._name}/ingest", json=body
        )
        resp.raise_for_status()
        data = resp.json()
        return IngestResult(
            nodes_written=data.get("nodes_written", 0),
            edges_written=data.get("edges_written", 0),
            rejected=data.get("rejected", 0),
        )

    async def label_source(self, external_id: str, labels: list[str]) -> None:
        body = {
            "mode": self._mode,
            "external_id": external_id,
            "labels": labels,
        }
        resp = await self._client.post(
            f"/v1/namespaces/{self._name}/sources/label", json=body
        )
        resp.raise_for_status()

    async def acquisition_execution(
        self,
        connectors: list[AcquisitionConnector],
        *,
        top_k: int = 0,
        min_gap_size: float = 0.0,
        max_gaps: int = 0,
        budget: int = 0,
        task_ids: list[str] | None = None,
        allowed_source_ids: list[str] | None = None,
        max_results: int = 0,
        max_attempts: int = 0,
        execute: bool = False,
    ) -> dict[str, Any]:
        """Preview or execute connector-specific acquisition workflows."""
        body: dict[str, Any] = {
            "mode": self._mode,
            "connectors": [
                {
                    "id": c.id,
                    "type": c.type,
                    "endpoint": c.endpoint,
                    "allowed_source_ids": c.allowed_source_ids,
                    "default_labels": c.default_labels,
                    "headers": c.headers,
                }
                for c in connectors
            ],
            "execute": execute,
        }
        if top_k:
            body["top_k"] = top_k
        if min_gap_size:
            body["min_gap_size"] = min_gap_size
        if max_gaps:
            body["max_gaps"] = max_gaps
        if budget:
            body["budget"] = budget
        if task_ids:
            body["task_ids"] = task_ids
        if allowed_source_ids:
            body["allowed_source_ids"] = allowed_source_ids
        if max_results:
            body["max_results"] = max_results
        if max_attempts:
            body["max_attempts"] = max_attempts

        resp = await self._client.post(
            f"/v1/namespaces/{self._name}/acquisition/execute", json=body
        )
        resp.raise_for_status()
        return resp.json()

    async def acquisition_execution_receipts(self, after: str = "") -> list[dict[str, Any]]:
        """Return append-only acquisition connector execution receipts."""
        params = {"mode": self._mode}
        if after:
            params["after"] = after
        resp = await self._client.get(
            f"/v1/namespaces/{self._name}/acquisition/receipts", params=params
        )
        resp.raise_for_status()
        return resp.json().get("receipts", [])

    async def acquisition_retry_candidates(self, after: str = "") -> list[dict[str, Any]]:
        """Return unresolved failed acquisition connector attempts."""
        params = {"mode": self._mode}
        if after:
            params["after"] = after
        resp = await self._client.get(
            f"/v1/namespaces/{self._name}/acquisition/retry-candidates", params=params
        )
        resp.raise_for_status()
        return resp.json().get("candidates", [])

    async def acquisition_retry_recommendations(self, after: str = "") -> list[dict[str, Any]]:
        """Return read-only acquisition retry backoff guidance."""
        params = {"mode": self._mode}
        if after:
            params["after"] = after
        resp = await self._client.get(
            f"/v1/namespaces/{self._name}/acquisition/retry-recommendations", params=params
        )
        resp.raise_for_status()
        return resp.json().get("recommendations", [])
