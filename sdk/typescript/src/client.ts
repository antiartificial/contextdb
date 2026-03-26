import type {
  WriteRequest,
  WriteResult,
  RetrieveRequest,
  Result,
  IngestResult,
} from './types';

/** Top-level client for a contextdb server. */
export class ContextDB {
  private baseUrl: string;

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
  }

  /** Return a namespace handle. */
  namespace(name: string, mode: string = 'general'): Namespace {
    return new Namespace(this.baseUrl, name, mode);
  }

  /** Health check. */
  async ping(): Promise<{ status: string }> {
    const resp = await fetch(`${this.baseUrl}/v1/ping`);
    if (!resp.ok) throw new Error(`ping failed: ${resp.status}`);
    return resp.json();
  }

  /** Get server stats. */
  async stats(): Promise<Record<string, unknown>> {
    const resp = await fetch(`${this.baseUrl}/v1/stats`);
    if (!resp.ok) throw new Error(`stats failed: ${resp.status}`);
    return resp.json();
  }
}

/** A namespace-scoped handle for reads and writes. */
export class Namespace {
  private baseUrl: string;
  private name: string;
  private mode: string;

  constructor(baseUrl: string, name: string, mode: string) {
    this.baseUrl = baseUrl;
    this.name = name;
    this.mode = mode;
  }

  /** Write a node to this namespace. */
  async write(req: WriteRequest): Promise<WriteResult> {
    const body: Record<string, unknown> = {
      mode: this.mode,
      content: req.content ?? '',
      source_id: req.sourceId ?? '',
    };
    if (req.labels) body.labels = req.labels;
    if (req.properties) body.properties = req.properties;
    if (req.vector) body.vector = req.vector;
    if (req.modelId) body.model_id = req.modelId;
    if (req.confidence) body.confidence = req.confidence;

    const resp = await fetch(
      `${this.baseUrl}/v1/namespaces/${this.name}/write`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      }
    );
    if (!resp.ok) throw new Error(`write failed: ${resp.status}`);
    const data = await resp.json();

    return {
      nodeId: data.node_id ?? '',
      admitted: data.admitted ?? false,
      reason: data.reason,
      conflictIds: data.conflict_ids,
    };
  }

  /** Retrieve nodes from this namespace. */
  async retrieve(req: RetrieveRequest): Promise<Result[]> {
    const body: Record<string, unknown> = {
      top_k: req.topK ?? 10,
    };
    if (req.vector) body.vector = req.vector;
    if (req.vectors) body.vectors = req.vectors;
    if (req.text) body.text = req.text;
    if (req.seedIds) body.seed_ids = req.seedIds;
    if (req.labels) body.labels = req.labels;
    if (req.scoreParams) {
      body.score_params = {
        similarity_weight: req.scoreParams.similarityWeight ?? 0,
        confidence_weight: req.scoreParams.confidenceWeight ?? 0,
        recency_weight: req.scoreParams.recencyWeight ?? 0,
        utility_weight: req.scoreParams.utilityWeight ?? 0,
        decay_alpha: req.scoreParams.decayAlpha ?? 0,
      };
    }

    const resp = await fetch(
      `${this.baseUrl}/v1/namespaces/${this.name}/retrieve`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      }
    );
    if (!resp.ok) throw new Error(`retrieve failed: ${resp.status}`);
    const data = await resp.json();

    return (data.results ?? []).map((r: Record<string, unknown>) => ({
      id: r.id ?? '',
      namespace: r.namespace ?? '',
      labels: (r.labels as string[]) ?? [],
      properties: (r.properties as Record<string, unknown>) ?? {},
      score: (r.score as number) ?? 0,
      similarityScore: (r.similarity_score as number) ?? 0,
      confidenceScore: (r.confidence_score as number) ?? 0,
      recencyScore: (r.recency_score as number) ?? 0,
      utilityScore: (r.utility_score as number) ?? 0,
      retrievalSource: (r.retrieval_source as string) ?? '',
    }));
  }

  /** Ingest raw text through the extraction pipeline. */
  async ingestText(text: string, sourceId: string = ''): Promise<IngestResult> {
    const body = {
      mode: this.mode,
      text,
      source_id: sourceId,
    };

    const resp = await fetch(
      `${this.baseUrl}/v1/namespaces/${this.name}/ingest`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      }
    );
    if (!resp.ok) throw new Error(`ingest failed: ${resp.status}`);
    const data = await resp.json();

    return {
      nodesWritten: data.nodes_written ?? 0,
      edgesWritten: data.edges_written ?? 0,
      rejected: data.rejected ?? 0,
    };
  }

  /** Set labels on a source. */
  async labelSource(externalId: string, labels: string[]): Promise<void> {
    const body = {
      mode: this.mode,
      external_id: externalId,
      labels,
    };

    const resp = await fetch(
      `${this.baseUrl}/v1/namespaces/${this.name}/sources/label`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      }
    );
    if (!resp.ok) throw new Error(`labelSource failed: ${resp.status}`);
  }
}
