<script lang="ts">
  type Metrics = {
    mode: string
    generated_at: string
    health: { status: string; signals: string[] }
    ingest: { total: number; admitted: number; rejected: number; admission_rate: number; rejection_rate: number }
    retrieval: { total: number; errors: number; error_rate: number }
    latency: { p95_ms: number; mean_ms: number }
  }

  type RankingResult = {
    rank: number
    node_id: string
    text?: string
    expected: boolean
    score: number
    similarity_score: number
    confidence_score: number
    recency_score: number
    utility_score: number
    retrieval_source?: string
  }

  type RankingQuery = {
    id: string
    description: string
    namespace: string
    category: string
    expected_rank_cutoff: number
    correct_rank?: number
    reciprocal_rank: number
    passed: boolean
    top_results: RankingResult[]
  }

  type RankingCategory = {
    category: string
    total_queries: number
    passed_queries: number
    failed_queries: number
    pass_rate: number
    mean_reciprocal_rank: number
  }

  type RankingEval = {
    generated_at: string
    contextdb_version: string
    corpus: string
    top_k: number
    total_queries: number
    passed_queries: number
    failed_queries: number
    mean_reciprocal_rank: number
    categories: RankingCategory[]
    queries: RankingQuery[]
  }

  type SearchResult = {
    id: string
    labels?: string[]
    text?: string
    source_id?: string
    confidence?: number
    match_reason: string
  }

  type EpistemicsSource = {
    external_id?: string
    labels?: string[]
    effective_credibility: number
    credibility_variance: number
    alpha?: number
    beta?: number
    claims_asserted?: number
    claims_validated?: number
    claims_refuted?: number
    updated_at?: string
  }

  type TimelinePoint = {
    time: string
    confidence?: number
    version?: number
    source_credibility?: number
    action?: string
    reason?: string
    node_id?: string
  }

  type ContradictionPath = {
    direction: string
    relation: string
    claim_id: string
    claim_text?: string
    other_node_id: string
    other_text?: string
    other_source_id?: string
    edge_weight: number
    severity: string
    confidence_gap: number
  }

  type GraphContextNode = {
    node_id: string
    text?: string
    source_id?: string
    labels?: string[]
    relation: string
    direction: string
    edge_type: string
    edge_weight: number
    confidence: number
    valid_from?: string
  }

  type EpistemicsSummary = {
    node_id: string
    namespace: string
    text?: string
    labels?: string[]
    source: EpistemicsSource
    confidence_timeline: TimelinePoint[]
    source_trust_timeline: TimelinePoint[]
    contradiction_paths: ContradictionPath[]
    graph_context: GraphContextNode[]
    counts: {
      supporters: number
      contradictors: number
      provenance: number
      graph_neighbors: number
      source_trust_points: number
      confidence_versions: number
    }
  }

  type BeliefAudit = {
    Node?: { ID: string; Confidence?: number; Version?: number; Labels?: string[]; Properties?: Record<string, unknown> }
    Source?: Record<string, unknown> | null
    Supporters?: unknown[]
    Contradictors?: unknown[]
    ProvenanceChain?: unknown[]
    ConfidenceHistory?: unknown[]
    AuditedAt?: string
    epistemics?: EpistemicsSummary
  }

  type RankExplanation = {
    margin: number
    summary: string
    factors: { factor: string; node_contribution: number; other_contribution: number; delta: number }[]
  }

  let activeTab = 'ranking'
  let metrics: Metrics | null = null
  let metricsError = ''
  let ranking: RankingEval | null = null
  let rankingError = ''
  let rankingTopK = '5'
  let selectedQueryID = ''
  let baselineName = ''
  let baseline: RankingEval | null = null
  let searchNamespace = 'default'
  let searchQuery = ''
  let searchLimit = '10'
  let searchResults: SearchResult[] = []
  let searchMessage = ''
  let debugNamespace = 'default'
  let debugID = ''
  let debugAudit: BeliefAudit | null = null
  let debugOutput =
    'Enter a namespace and node ID to inspect source, support, contradictions, provenance, and confidence history.'
  let compareLeft = ''
  let compareRight = ''
  let compareQuery = ''
  let compareOutput = 'Select two search results or paste node IDs to compare ranking factors.'
  let compareSummary = ''

  const tabs = ['ranking', 'metrics', 'debugger', 'compare'] as const
  const percent = (value = 0) => `${Math.round(value * 1000) / 10}%`
  const ms = (value = 0) => `${value.toFixed(2)} ms`
  const score = (value = 0) => value.toFixed(3)
  const width = (value = 0) => `${Math.max(0, Math.min(100, value * 100))}%`
  const rankText = (query: RankingQuery) => query.correct_rank ? `#${query.correct_rank}` : 'miss'
  const topResult = (query: RankingQuery | null) => query?.top_results?.[0]
  const resultLabel = (result?: RankingResult) => result?.text || result?.node_id || 'No result'
  const shortID = (value = '') => value.length > 12 ? `${value.slice(0, 8)}...${value.slice(-4)}` : value
  const formatDate = (value = '') => value ? new Date(value).toLocaleString() : 'n/a'
  const credibilityLabel = (value = 0) => value >= 0.8 ? 'trusted' : value >= 0.45 ? 'mixed' : 'low trust'
  const timelineLeft = (index: number, total: number) => total <= 1 ? '50%' : `${(index / (total - 1)) * 100}%`
  const timelineValue = (point: TimelinePoint) => point.source_credibility ?? point.confidence ?? 0
  const timelineTitle = (point: TimelinePoint) => `${formatDate(point.time)} · ${score(timelineValue(point))}${point.action ? ` · ${point.action}` : ''}`

  $: selectedQuery =
    ranking?.queries.find((query) => query.id === selectedQueryID) || ranking?.queries[0] || null
  $: if (ranking && !selectedQueryID && ranking.queries.length > 0) {
    selectedQueryID = ranking.queries[0].id
  }
  $: baselineDelta = baseline && ranking ? {
    mrr: ranking.mean_reciprocal_rank - baseline.mean_reciprocal_rank,
    passed: ranking.passed_queries - baseline.passed_queries,
    failed: ranking.failed_queries - baseline.failed_queries
  } : null

  async function refreshMetrics() {
    try {
      const response = await fetch('/admin/api/metrics')
      const text = await response.text()
      if (!response.ok) throw new Error(text || response.statusText)
      metrics = JSON.parse(text)
      metricsError = ''
    } catch (error) {
      metricsError = String(error instanceof Error ? error.message : error)
    }
  }

  async function runRankingEval() {
    rankingError = 'Running ranking evaluation...'
    try {
      const params = new URLSearchParams({ top_k: rankingTopK.trim() || '5' })
      const response = await fetch(`/admin/api/ranking-eval?${params}`)
      const text = await response.text()
      if (!response.ok) throw new Error(text || response.statusText)
      ranking = JSON.parse(text)
      selectedQueryID = ranking?.queries[0]?.id || ''
      rankingError = ''
    } catch (error) {
      rankingError = String(error instanceof Error ? error.message : error)
    }
  }

  async function loadBaseline(event: Event) {
    const input = event.currentTarget as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    try {
      baseline = JSON.parse(await file.text())
      baselineName = file.name
    } catch (error) {
      baselineName = ''
      baseline = null
      rankingError = String(error instanceof Error ? error.message : error)
    }
  }

  async function runSearch() {
    searchMessage = 'Searching...'
    searchResults = []
    try {
      const params = new URLSearchParams({
        ns: searchNamespace.trim(),
        q: searchQuery.trim(),
        limit: searchLimit.trim(),
      })
      const response = await fetch(`/admin/api/search?${params}`)
      const text = await response.text()
      if (!response.ok) throw new Error(text || response.statusText)
      const data = JSON.parse(text)
      searchResults = data.results || []
      searchMessage = searchResults.length === 0 ? 'No matching nodes.' : ''
    } catch (error) {
      searchMessage = String(error instanceof Error ? error.message : error)
    }
  }

  async function inspectResult(result: SearchResult) {
    activeTab = 'debugger'
    debugNamespace = searchNamespace
    debugID = result.id
    await inspectNode()
  }

  function addCompareTarget(result: SearchResult) {
    activeTab = 'compare'
    if (!compareLeft || compareLeft === result.id) {
      compareLeft = result.id
      return
    }
    compareRight = result.id
  }

  async function inspectNode() {
    debugOutput = 'Loading...'
    debugAudit = null
    try {
      const params = new URLSearchParams({ ns: debugNamespace.trim(), id: debugID.trim() })
      const response = await fetch(`/admin/api/belief?${params}`)
      const text = await response.text()
      if (!response.ok) throw new Error(text || response.statusText)
      debugAudit = JSON.parse(text)
      debugOutput = JSON.stringify(debugAudit, null, 2)
    } catch (error) {
      debugOutput = String(error instanceof Error ? error.message : error)
    }
  }

  async function inspectGraphNode(node: GraphContextNode) {
    debugID = node.node_id
    await inspectNode()
  }

  function compareContradiction(path: ContradictionPath) {
    activeTab = 'compare'
    searchNamespace = debugNamespace
    compareLeft = path.claim_id
    compareRight = path.other_node_id
    compareQuery = path.claim_text || path.other_text || ''
  }

  async function compareRank() {
    compareOutput = 'Comparing...'
    compareSummary = ''
    try {
      const response = await fetch('/admin/api/explain-rank', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          namespace: searchNamespace.trim(),
          node_id: compareLeft.trim(),
          other_node_id: compareRight.trim(),
          text: compareQuery.trim(),
          max_depth: 2,
        }),
      })
      const text = await response.text()
      if (!response.ok) throw new Error(text || response.statusText)
      const data: RankExplanation = JSON.parse(text)
      compareSummary = data.summary
      compareOutput = JSON.stringify(data, null, 2)
    } catch (error) {
      compareOutput = String(error instanceof Error ? error.message : error)
    }
  }

  refreshMetrics()
  runRankingEval()
  const interval = window.setInterval(refreshMetrics, 5000)
</script>

<svelte:window on:beforeunload={() => window.clearInterval(interval)} />

<main class="shell">
  <header class="topbar">
    <div>
      <h1>contextdb admin</h1>
      <p>Ranking evaluation, metrics, debugger search, and belief evidence for local contextdb observability.</p>
    </div>
    <div class="health">
      <span>Health</span>
      <strong>{metrics?.health.status || 'loading'}</strong>
      <small>{metrics?.generated_at || 'waiting for metrics'}</small>
    </div>
  </header>

  <nav class="tabs" aria-label="Admin workspace">
    {#each tabs as tab}
      <button type="button" class:active={activeTab === tab} on:click={() => (activeTab = tab)}>
        {tab === 'ranking' ? 'Ranking Evaluation' : tab === 'metrics' ? 'Metrics' : tab === 'debugger' ? 'Belief Debugger' : 'Explain Rank'}
      </button>
    {/each}
  </nav>

  {#if activeTab === 'ranking'}
    <section class="ranking-workspace" aria-labelledby="ranking-heading">
      <div class="section-heading">
        <div>
          <h2 id="ranking-heading">Ranking Evaluation</h2>
          <p>Representative corpus score drift, pass/fail posture, and query-level evidence.</p>
        </div>
        <div class="ranking-actions">
          <label>
            Top K
            <select bind:value={rankingTopK}>
              <option value="3">3</option>
              <option value="5">5</option>
              <option value="10">10</option>
              <option value="25">25</option>
            </select>
          </label>
          <label class="file-control">
            Baseline JSON
            <input type="file" accept="application/json,.json" on:change={loadBaseline} />
          </label>
          <button type="button" on:click={runRankingEval}>Run Eval</button>
        </div>
      </div>

      {#if rankingError}
        <p class:muted={rankingError.includes('Running')} class:error={!rankingError.includes('Running')}>{rankingError}</p>
      {/if}

      <div class="rank-summary">
        <article>
          <span>MRR</span>
          <strong>{score(ranking?.mean_reciprocal_rank)}</strong>
          <small>{baselineDelta ? `${baselineDelta.mrr >= 0 ? '+' : ''}${score(baselineDelta.mrr)} vs ${baselineName}` : 'current run'}</small>
        </article>
        <article>
          <span>Passed</span>
          <strong>{ranking?.passed_queries ?? 0}/{ranking?.total_queries ?? 0}</strong>
          <small>{ranking ? percent(ranking.passed_queries / ranking.total_queries) : 'waiting'}</small>
        </article>
        <article>
          <span>Failed</span>
          <strong>{ranking?.failed_queries ?? 0}</strong>
          <small>{baselineDelta ? `${baselineDelta.failed >= 0 ? '+' : ''}${baselineDelta.failed} vs baseline` : 'cutoff-aware'}</small>
        </article>
        <article>
          <span>Corpus</span>
          <strong>{ranking?.corpus || 'representative'}</strong>
          <small>{ranking?.generated_at || 'not run yet'}</small>
        </article>
      </div>

      <div class="ranking-grid">
        <section class="panel category-panel">
          <h3>Category Health</h3>
          {#each ranking?.categories || [] as category}
            <div class="category-row">
              <div>
                <strong>{category.category}</strong>
                <span>{category.passed_queries}/{category.total_queries} passed · MRR {score(category.mean_reciprocal_rank)}</span>
              </div>
              <div class="bar-track"><span style={`width:${width(category.pass_rate)}`}></span></div>
            </div>
          {/each}
        </section>

        <section class="panel diff-panel">
          <h3>Release Diff</h3>
          {#if baselineDelta}
            <dl>
              <div><dt>MRR delta</dt><dd>{baselineDelta.mrr >= 0 ? '+' : ''}{score(baselineDelta.mrr)}</dd></div>
              <div><dt>Passed delta</dt><dd>{baselineDelta.passed >= 0 ? '+' : ''}{baselineDelta.passed}</dd></div>
              <div><dt>Failed delta</dt><dd>{baselineDelta.failed >= 0 ? '+' : ''}{baselineDelta.failed}</dd></div>
            </dl>
          {:else}
            <p class="muted">Load a previous `ranking-eval-v*.json` artifact to compare this run in the browser.</p>
          {/if}
        </section>
      </div>

      <div class="ranking-main">
        <section class="panel query-table">
          <div class="table-head">
            <span>Query</span>
            <span>Category</span>
            <span>Status</span>
            <span>Rank</span>
            <span>MRR</span>
            <span>Top result</span>
            <span>Score</span>
          </div>
          {#each ranking?.queries || [] as query}
            <button type="button" class="query-row" class:selected={selectedQuery?.id === query.id} on:click={() => (selectedQueryID = query.id)}>
              <span>
                <strong>{query.id}</strong>
                <small>{query.description}</small>
              </span>
              <span>{query.category}</span>
              <span class:pass={query.passed} class:fail={!query.passed}>{query.passed ? 'pass' : 'fail'}</span>
              <span>{rankText(query)} / {query.expected_rank_cutoff}</span>
              <span>{score(query.reciprocal_rank)}</span>
              <span>{resultLabel(topResult(query))}</span>
              <span>{score(topResult(query)?.score)}</span>
            </button>
          {/each}
        </section>

        <aside class="panel query-detail">
          <h3>{selectedQuery?.id || 'Select a query'}</h3>
          {#if selectedQuery}
            <p>{selectedQuery.description}</p>
            <div class="detail-meta">
              <span>{selectedQuery.namespace}</span>
              <span>{selectedQuery.category}</span>
              <span>{selectedQuery.passed ? 'pass' : 'fail'}</span>
            </div>
            {#if topResult(selectedQuery)}
              <h4>Top Result</h4>
              <strong class="result-title">{resultLabel(topResult(selectedQuery))}</strong>
              <div class="score-bars">
                <div><span>Similarity</span><div class="bar-track"><span style={`width:${width(topResult(selectedQuery)?.similarity_score)}`}></span></div></div>
                <div><span>Confidence</span><div class="bar-track"><span style={`width:${width(topResult(selectedQuery)?.confidence_score)}`}></span></div></div>
                <div><span>Recency</span><div class="bar-track"><span style={`width:${width(topResult(selectedQuery)?.recency_score)}`}></span></div></div>
                <div><span>Utility</span><div class="bar-track"><span style={`width:${width(topResult(selectedQuery)?.utility_score)}`}></span></div></div>
              </div>
            {/if}
            <pre>{JSON.stringify(selectedQuery, null, 2)}</pre>
          {/if}
        </aside>
      </div>
    </section>
  {/if}

  {#if activeTab === 'metrics'}
    <section class="dashboard" aria-labelledby="metrics-heading">
      <div class="section-heading">
        <div>
          <h2 id="metrics-heading">Metrics</h2>
          <p>Runtime health, traffic, and latency from the local process.</p>
        </div>
        <button type="button" on:click={refreshMetrics}>Refresh</button>
      </div>
      <div class="metrics-grid">
        <article class="metric"><span>Mode</span><strong>{metrics?.mode || 'embedded'}</strong><small>Runtime store profile</small></article>
        <article class="metric"><span>Ingest Total</span><strong>{metrics?.ingest.total ?? 0}</strong><small>{percent(metrics?.ingest.admission_rate)} admitted</small></article>
        <article class="metric"><span>Retrieval Total</span><strong>{metrics?.retrieval.total ?? 0}</strong><small>{percent(metrics?.retrieval.error_rate)} error rate</small></article>
        <article class="metric"><span>P95 Latency</span><strong>{ms(metrics?.latency.p95_ms)}</strong><small>Mean {ms(metrics?.latency.mean_ms)}</small></article>
      </div>
      <div class="metrics-detail">
        <div class="panel bars">
          <div><div class="bar-label"><span>Admitted</span><span>{metrics?.ingest.admitted ?? 0} / {metrics?.ingest.total ?? 0}</span></div><div class="bar-track"><span style={`width:${width(metrics?.ingest.admission_rate)}`}></span></div></div>
          <div><div class="bar-label"><span>Rejected</span><span>{metrics?.ingest.rejected ?? 0} / {metrics?.ingest.total ?? 0}</span></div><div class="bar-track warn"><span style={`width:${width(metrics?.ingest.rejection_rate)}`}></span></div></div>
          <div><div class="bar-label"><span>Retrieval errors</span><span>{metrics?.retrieval.errors ?? 0} / {metrics?.retrieval.total ?? 0}</span></div><div class="bar-track warn"><span style={`width:${width(metrics?.retrieval.error_rate)}`}></span></div></div>
        </div>
        <div class="panel">
          <h3>Signals</h3>
          {#if metricsError}<p class="error">{metricsError}</p>{:else}<ul class="signals">{#each metrics?.health.signals || ['Loading metrics...'] as signal}<li>{signal}</li>{/each}</ul>{/if}
          <pre>{metrics ? JSON.stringify(metrics, null, 2) : 'Loading /admin/api/metrics...'}</pre>
        </div>
      </div>
    </section>
  {/if}

  {#if activeTab === 'debugger'}
    <section class="debugger" aria-labelledby="debugger-heading">
      <div class="section-heading">
        <div>
          <h2 id="debugger-heading">Belief Debugger</h2>
          <p>Search claims, inspect epistemic evidence, and follow source trust or contradiction paths.</p>
        </div>
      </div>
      <div class="tool">
        <div class="search-row">
          <label>Namespace<input bind:value={searchNamespace} autocomplete="off" /></label>
          <label>Search<input bind:value={searchQuery} placeholder="text, source, label, or UUID" autocomplete="off" /></label>
          <label>Limit<input bind:value={searchLimit} inputmode="numeric" autocomplete="off" /></label>
          <button type="button" on:click={runSearch}>Search</button>
        </div>
        {#if searchMessage || searchResults.length > 0}
          <div class="results">
            {#if searchMessage}<p>{searchMessage}</p>{/if}
            {#each searchResults as result}
              <article class="result">
                <div><strong>{result.text || result.id}</strong><span>{result.id} | {result.source_id || 'unknown source'} | confidence {(result.confidence || 0).toFixed(2)} | {result.match_reason}</span></div>
                <div class="result-actions"><button type="button" class="ghost" on:click={() => inspectResult(result)}>Inspect</button><button type="button" class="ghost" on:click={() => addCompareTarget(result)}>Compare</button></div>
              </article>
            {/each}
          </div>
        {/if}
        <div class="inspect-row">
          <label>Namespace<input bind:value={debugNamespace} autocomplete="off" /></label>
          <label>Node ID<input bind:value={debugID} placeholder="00000000-0000-0000-0000-000000000000" autocomplete="off" /></label>
          <button type="button" on:click={inspectNode}>Inspect</button>
        </div>

        {#if debugAudit?.epistemics}
          <div class="epistemics-workbench">
            <section class="panel evidence-claim">
              <div>
                <span class="eyebrow">Selected claim</span>
                <h3>{debugAudit.epistemics.text || debugAudit.epistemics.node_id}</h3>
                <p>{debugAudit.epistemics.namespace} · {shortID(debugAudit.epistemics.node_id)}</p>
              </div>
              <div class="evidence-score">
                <span>Confidence</span>
                <strong>{score(debugAudit.Node?.Confidence || 0)}</strong>
                <small>v{debugAudit.Node?.Version || 0}</small>
              </div>
            </section>

            <section class="evidence-metrics">
              <article><span>Support</span><strong>{debugAudit.epistemics.counts.supporters}</strong><small>incoming evidence</small></article>
              <article><span>Contradictions</span><strong>{debugAudit.epistemics.counts.contradictors}</strong><small>conflict paths</small></article>
              <article><span>Provenance</span><strong>{debugAudit.epistemics.counts.provenance}</strong><small>derived chain</small></article>
              <article><span>Neighbors</span><strong>{debugAudit.epistemics.counts.graph_neighbors}</strong><small>graph context</small></article>
            </section>

            <div class="epistemics-grid">
              <section class="panel source-panel">
                <div class="panel-title">
                  <h3>Source Context</h3>
                  <span>{credibilityLabel(debugAudit.epistemics.source.effective_credibility)}</span>
                </div>
                <strong>{debugAudit.epistemics.source.external_id || 'unknown source'}</strong>
                <div class="source-score">
                  <span>Credibility</span>
                  <strong>{score(debugAudit.epistemics.source.effective_credibility)}</strong>
                  <div class="bar-track"><span style={`width:${width(debugAudit.epistemics.source.effective_credibility)}`}></span></div>
                </div>
                <dl>
                  <div><dt>Beta</dt><dd>{score(debugAudit.epistemics.source.alpha || 0)} / {score(debugAudit.epistemics.source.beta || 0)}</dd></div>
                  <div><dt>Variance</dt><dd>{score(debugAudit.epistemics.source.credibility_variance)}</dd></div>
                  <div><dt>Validated</dt><dd>{debugAudit.epistemics.source.claims_validated || 0}</dd></div>
                  <div><dt>Refuted</dt><dd>{debugAudit.epistemics.source.claims_refuted || 0}</dd></div>
                </dl>
                <div class="chip-row">
                  {#each debugAudit.epistemics.source.labels || ['unlabeled'] as label}
                    <span>{label}</span>
                  {/each}
                </div>
              </section>

              <section class="panel timeline-panel">
                <div class="panel-title">
                  <h3>Source Trust Timeline</h3>
                  <span>{debugAudit.epistemics.counts.source_trust_points} points</span>
                </div>
                {#if debugAudit.epistemics.source_trust_timeline.length > 0}
                  <div class="timeline-rail">
                    {#each debugAudit.epistemics.source_trust_timeline as point, index}
                      <button type="button" class="timeline-point" title={timelineTitle(point)} style={`left:${timelineLeft(index, debugAudit.epistemics.source_trust_timeline.length)}; bottom:${Math.max(8, timelineValue(point) * 82)}%`}>
                        <span>{score(timelineValue(point))}</span>
                      </button>
                    {/each}
                  </div>
                  <div class="timeline-events">
                    {#each debugAudit.epistemics.source_trust_timeline.slice(-4) as point}
                      <div><strong>{point.action}</strong><span>{score(timelineValue(point))} · {formatDate(point.time)}</span></div>
                    {/each}
                  </div>
                {:else}
                  <p class="muted">No source feedback events yet. Validate or refute claims from this source to build a trust trace.</p>
                {/if}
              </section>

              <section class="panel timeline-panel">
                <div class="panel-title">
                  <h3>Confidence History</h3>
                  <span>{debugAudit.epistemics.counts.confidence_versions} versions</span>
                </div>
                {#if debugAudit.epistemics.confidence_timeline.length > 0}
                  <div class="timeline-rail confidence">
                    {#each debugAudit.epistemics.confidence_timeline as point, index}
                      <button type="button" class="timeline-point" title={timelineTitle(point)} style={`left:${timelineLeft(index, debugAudit.epistemics.confidence_timeline.length)}; bottom:${Math.max(8, timelineValue(point) * 82)}%`}>
                        <span>v{point.version}</span>
                      </button>
                    {/each}
                  </div>
                {:else}
                  <p class="muted">No version history is available for this node.</p>
                {/if}
              </section>
            </div>

            <div class="epistemics-main">
              <section class="panel contradiction-panel">
                <div class="panel-title">
                  <h3>Contradiction Paths</h3>
                  <span>{debugAudit.epistemics.contradiction_paths.length} paths</span>
                </div>
                {#if debugAudit.epistemics.contradiction_paths.length > 0}
                  {#each debugAudit.epistemics.contradiction_paths as path}
                    <article class="path-row">
                      <div>
                        <span class={`severity ${path.severity}`}>{path.severity}</span>
                        <strong>{path.relation}</strong>
                        <p>{path.other_text || path.other_node_id}</p>
                        <small>{shortID(path.claim_id)} -> {path.edge_weight ? score(path.edge_weight) : '1.000'} -> {shortID(path.other_node_id)} · gap {score(path.confidence_gap)}</small>
                      </div>
                      <button type="button" class="ghost" on:click={() => compareContradiction(path)}>Compare</button>
                    </article>
                  {/each}
                {:else}
                  <p class="muted">No contradiction edges were found for this claim.</p>
                {/if}
              </section>

              <section class="panel graph-panel">
                <div class="panel-title">
                  <h3>Graph & Source Context</h3>
                  <span>{debugAudit.epistemics.graph_context.length} neighbors</span>
                </div>
                {#if debugAudit.epistemics.graph_context.length > 0}
                  <div class="graph-list">
                    {#each debugAudit.epistemics.graph_context as node}
                      <button type="button" class="graph-row" on:click={() => inspectGraphNode(node)}>
                        <span class="relation">{node.relation}</span>
                        <strong>{node.text || node.node_id}</strong>
                        <small>{shortID(node.node_id)} · {node.source_id || 'unknown source'} · confidence {score(node.confidence)}</small>
                      </button>
                    {/each}
                  </div>
                {:else}
                  <p class="muted">No active graph neighbors were found.</p>
                {/if}
              </section>
            </div>
          </div>
        {/if}

        <details class="raw-audit">
          <summary>Raw audit JSON</summary>
          <pre>{debugOutput}</pre>
        </details>
      </div>
    </section>
  {/if}

  {#if activeTab === 'compare'}
    <section class="compare" aria-labelledby="compare-heading">
      <h2 id="compare-heading">Explain Rank Compare</h2>
      <div class="tool">
        <div class="compare-row">
          <label>Left node<input bind:value={compareLeft} placeholder="Node UUID" autocomplete="off" /></label>
          <label>Right node<input bind:value={compareRight} placeholder="Node UUID" autocomplete="off" /></label>
          <label>Query text<input bind:value={compareQuery} placeholder="optional query context" autocomplete="off" /></label>
          <button type="button" on:click={compareRank}>Compare</button>
        </div>
        {#if compareSummary}<p class="compare-summary">{compareSummary}</p>{/if}
        <pre>{compareOutput}</pre>
      </div>
    </section>
  {/if}

  <section class="links" aria-labelledby="links-heading">
    <h2 id="links-heading">Quick Links</h2>
    <a href="/v1/ping">Health Check</a>
    <a href="/v1/stats">API Stats</a>
    <a href="/metrics">Prometheus Metrics</a>
    <a href="/debug/pprof/">Profiling</a>
    <a href="/debug/vars">expvar</a>
  </section>
</main>
