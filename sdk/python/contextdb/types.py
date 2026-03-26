"""Type definitions for the contextdb Python SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass
class WriteRequest:
    """Request to write a node."""

    content: str = ""
    source_id: str = ""
    labels: list[str] = field(default_factory=list)
    properties: dict[str, str] = field(default_factory=dict)
    vector: list[float] | None = None
    model_id: str = ""
    confidence: float = 0.0
    valid_from: str | None = None
    mem_type: str = ""


@dataclass
class WriteResult:
    """Result of a write operation."""

    node_id: str = ""
    admitted: bool = False
    reason: str = ""
    conflict_ids: list[str] = field(default_factory=list)


@dataclass
class RetrieveRequest:
    """Request to retrieve nodes."""

    vector: list[float] | None = None
    vectors: list[list[float]] | None = None
    text: str = ""
    seed_ids: list[str] = field(default_factory=list)
    top_k: int = 10
    labels: list[str] = field(default_factory=list)
    score_params: ScoreParams | None = None


@dataclass
class ScoreParams:
    """Scoring parameter overrides."""

    similarity_weight: float = 0.0
    confidence_weight: float = 0.0
    recency_weight: float = 0.0
    utility_weight: float = 0.0
    decay_alpha: float = 0.0


@dataclass
class Result:
    """A single retrieval result."""

    id: str = ""
    namespace: str = ""
    labels: list[str] = field(default_factory=list)
    properties: dict[str, Any] = field(default_factory=dict)
    score: float = 0.0
    similarity_score: float = 0.0
    confidence_score: float = 0.0
    recency_score: float = 0.0
    utility_score: float = 0.0
    retrieval_source: str = ""


@dataclass
class IngestResult:
    """Result of a text ingestion."""

    nodes_written: int = 0
    edges_written: int = 0
    rejected: int = 0
