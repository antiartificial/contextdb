import type { Figure, FigRow } from './vendor/model'

// One example runs through all three figures. These are present-tense flows;
// planned lexical search, RRF, natural-language time parsing, and refreshable
// derived summaries are intentionally absent.
const CLAIM: FigRow[] = [{ tag: 'claim', tone: 'blue', text: 'Deploys use canary rollout' }]
const SOURCE: FigRow[] = [{ tag: 'source', tone: 'purple', text: 'Release runbook', meta: 'credibility 0.8' }]

export const writeFigure: Figure = {
  title: 'A claim enters ContextDB',
  props: {
    speed: 1600,
    layout: {
      gap: 34,
      children: [
        { id: 'app', label: 'Application', lines: 2, width: 172 },
        {
          id: 'write', label: 'Write path', direction: 'column', gap: 24,
          children: [
            { id: 'embed', label: 'Embed', sub: 'if configured' },
            { id: 'source', label: 'Source', sub: 'credibility' },
            { id: 'admit', label: 'Admission gate', sub: 'novelty + trust', shape: 'decision' },
            { id: 'conflict', label: 'Conflict detector', sub: 'similar claims' },
          ],
        },
        {
          id: 'stores', label: 'Stored evidence', direction: 'column', gap: 22,
          children: [
            { id: 'graph', label: 'Graph', shape: 'store', lines: 3, width: 182 },
            { id: 'vectors', label: 'Vector index', shape: 'store' },
            { id: 'events', label: 'Event log', shape: 'store' },
          ],
        },
      ],
    },
    edges: [
      { id: 'submit', from: 'app', to: 'embed', label: 'write' },
      { id: 'resolve', from: 'embed', to: 'source' },
      { id: 'gate', from: 'source', to: 'admit' },
      { id: 'check', from: 'admit', to: 'conflict' },
      { id: 'persist', from: 'conflict', to: 'graph' },
      { id: 'index', from: 'graph', to: 'vectors' },
      { id: 'record', from: 'graph', to: 'events' },
    ],
    steps: [
      {
        label: 'Submit', caption: 'An application writes a claim with a source.',
        flow: [
          { edges: { edge: 'submit', data: 'claim + source' }, show: { app: CLAIM }, say: 'The application supplies content and a source ID. If no vector is supplied, a configured embedder creates one.' },
          { edges: 'resolve', show: { source: SOURCE }, say: 'ContextDB resolves the source and its credibility. A new source starts neutral.' },
        ],
      },
      {
        label: 'Check', caption: 'Trust, novelty, and nearby claims decide admission.',
        flow: [
          { edges: 'gate', show: { admit: [{ tag: 'check', tone: 'orange', text: 'credible + novel?', mark: '✓' }] }, say: 'The admission gate can reject low-trust, near-duplicate, or low-novelty content.' },
          { edges: 'check', show: { conflict: [{ tag: 'compare', tone: 'orange', text: 'Existing: blue-green rollout', meta: 'possible conflict' }] }, say: 'For an admitted claim, similar neighbors are checked for contradictions.' },
        ],
      },
      {
        label: 'Persist', caption: 'The admitted claim and its conflict stay inspectable.',
        flow: [
          { edges: 'persist', show: { graph: [...CLAIM, { tag: 'edge', tone: 'orange', text: 'contradicts earlier claim' }] }, say: 'The graph stores the claim and any confirmed contradiction edge.' },
          { edges: ['index', 'record'], show: { vectors: [{ tag: 'index', tone: 'blue', text: 'claim embedding' }], events: [{ tag: 'event', tone: 'gray', text: 'write recorded' }] }, say: 'The vector index supports search; the event log records the write.' },
        ],
      },
    ],
  },
}

export const retrieveFigure: Figure = {
  title: 'A question finds and ranks evidence',
  props: {
    speed: 1600,
    layout: {
      gap: 32,
      children: [
        { id: 'question', label: 'Question', lines: 2, width: 170 },
        {
          id: 'paths', label: 'Candidate paths', direction: 'column', gap: 18,
          children: [
            { id: 'vector', label: 'Vector search', sub: 'similar meaning' },
            { id: 'graph', label: 'Graph walk', sub: 'from seed IDs' },
            { id: 'session', label: 'Session context', sub: 'recent nodes' },
          ],
        },
        {
          id: 'ranking', label: 'Ranking', direction: 'column', gap: 20,
          children: [
            { id: 'fusion', label: 'Merge', sub: 'deduplicate + weight' },
            { id: 'score', label: 'Score', sub: 'four contributions' },
            { id: 'result', label: 'Ranked results', lines: 3, width: 192 },
          ],
        },
      ],
    },
    edges: [
      { id: 'qv', from: 'question', to: 'vector' },
      { id: 'qg', from: 'question', to: 'graph' },
      { id: 'qs', from: 'question', to: 'session' },
      { id: 'vf', from: 'vector', to: 'fusion' },
      { id: 'gf', from: 'graph', to: 'fusion' },
      { id: 'sf', from: 'session', to: 'fusion' },
      { id: 'fs', from: 'fusion', to: 'score' },
      { id: 'sr', from: 'score', to: 'result' },
    ],
    steps: [
      {
        label: 'Find', caption: 'One question can gather candidates from several paths.',
        flow: [
          { edges: ['qv', 'qg', 'qs'], show: { question: [{ tag: 'query', tone: 'blue', text: 'How do we deploy?' }] }, say: 'Text is embedded when an embedder is configured. Graph traversal uses supplied seed IDs; session context uses recent nodes when available.' },
          { edges: ['vf', 'gf', 'sf'], show: { fusion: [{ tag: 'candidate', tone: 'blue', text: 'Canary rollout' }, { tag: 'candidate', tone: 'purple', text: 'Release runbook' }] }, say: 'Candidates from available paths are merged and deduplicated by node ID.' },
        ],
      },
      {
        label: 'Rank', caption: 'A result carries an inspectable score.',
        flow: [
          { edges: 'fs', show: { score: [{ tag: 'score', tone: 'green', text: 'similarity' }, { text: 'confidence · recency · utility' }] }, say: 'ContextDB combines similarity, confidence, recency, and utility. An optional reranker can change the final order.' },
          { edges: 'sr', show: { result: [{ tag: '1', tone: 'green', text: 'Canary rollout', mark: '✓' }, { tag: '2', tone: 'blue', text: 'Release runbook' }] }, say: 'Default scoring exposes weighted contributions. An optional reranker returns its own scores, so its explanation differs.' },
        ],
      },
    ],
  },
}

export const reviewFigure: Figure = {
  title: 'A disagreement becomes a reviewable decision',
  props: {
    speed: 1600,
    layout: {
      gap: 34,
      children: [
        {
          id: 'evidence', label: 'Evidence', direction: 'column', gap: 18,
          children: [
            { id: 'old', label: 'Earlier claim', lines: 2, width: 174 },
            { id: 'new', label: 'New claim', lines: 2, width: 174 },
          ],
        },
        {
          id: 'process', label: 'Review path', direction: 'column', gap: 18,
          children: [
            { id: 'contradiction', label: 'Contradiction', sub: 'graph edge' },
            { id: 'feedback', label: 'Feedback', sub: 'durable event' },
            { id: 'queue', label: 'Review queue', sub: 'ranked task' },
          ],
        },
        { id: 'reviewer', label: 'Reviewer', lines: 3, width: 184 },
      ],
    },
    edges: [
      { id: 'old-c', from: 'old', to: 'contradiction' },
      { id: 'new-c', from: 'new', to: 'contradiction' },
      { id: 'c-f', from: 'contradiction', to: 'feedback' },
      { id: 'c-q', from: 'contradiction', to: 'queue' },
      { id: 'f-q', from: 'feedback', to: 'queue' },
      { id: 'q-r', from: 'queue', to: 'reviewer' },
    ],
    steps: [
      {
        label: 'Disagree', caption: 'Contradictory claims remain visible with their sources.',
        flow: [
          { edges: ['old-c', 'new-c'], show: { old: [{ tag: 'claim', tone: 'purple', text: 'Deploys use blue-green' }], new: CLAIM, contradiction: [{ tag: 'edge', tone: 'orange', text: 'contradicts' }] }, say: 'A confirmed contradiction links the claims. Neither is silently deleted or declared true.' },
        ],
      },
      {
        label: 'Signal', caption: 'Contradictions or explicit feedback can produce a task.',
        flow: [
          { edges: 'c-q', show: { queue: [{ tag: 'task', tone: 'orange', text: 'Review conflicting claims' }] }, say: 'A contradiction can produce a review task directly. It does not require someone to submit feedback first.' },
          { edges: ['c-f', 'f-q'], show: { feedback: [{ tag: 'event', tone: 'orange', text: 'explicit refute / stale / useful' }] }, say: 'If a caller submits feedback, ContextDB records a durable event that can also affect source credibility and review priority.' },
        ],
      },
      {
        label: 'Resolve', caption: 'A reviewer records a decision with an audit trail.',
        flow: [
          { edges: 'q-r', show: { reviewer: [{ tag: 'action', tone: 'green', text: 'Inspect evidence' }, { tag: 'record', tone: 'blue', text: 'Assign, note, or resolve' }] }, say: 'An operator inspects the evidence and records a workflow decision. ContextDB does not automatically choose the winning claim.' },
        ],
      },
    ],
  },
}

export const figures = { write: writeFigure, retrieve: retrieveFigure, review: reviewFigure } as const
