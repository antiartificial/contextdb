/** Request to write a node. */
export interface WriteRequest {
  content?: string;
  sourceId?: string;
  labels?: string[];
  properties?: Record<string, string>;
  vector?: number[];
  modelId?: string;
  confidence?: number;
}

/** Result of a write operation. */
export interface WriteResult {
  nodeId: string;
  admitted: boolean;
  reason?: string;
  conflictIds?: string[];
}

/** Request to retrieve nodes. */
export interface RetrieveRequest {
  vector?: number[];
  vectors?: number[][];
  text?: string;
  seedIds?: string[];
  topK?: number;
  labels?: string[];
  scoreParams?: ScoreParams;
}

/** Scoring parameter overrides. */
export interface ScoreParams {
  similarityWeight?: number;
  confidenceWeight?: number;
  recencyWeight?: number;
  utilityWeight?: number;
  decayAlpha?: number;
}

/** A single retrieval result. */
export interface Result {
  id: string;
  namespace: string;
  labels: string[];
  properties: Record<string, unknown>;
  score: number;
  similarityScore: number;
  confidenceScore: number;
  recencyScore: number;
  utilityScore: number;
  retrievalSource: string;
}

/** Result of a text ingestion. */
export interface IngestResult {
  nodesWritten: number;
  edgesWritten: number;
  rejected: number;
}
