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

  function inspectResult(result: SearchResult) {
    activeTab = 'debugger'
    debugNamespace = searchNamespace
    debugID = result.id
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
    try {
      const params = new URLSearchParams({ ns: debugNamespace.trim(), id: debugID.trim() })
      const response = await fetch(`/admin/api/belief?${params}`)
      const text = await response.text()
      if (!response.ok) throw new Error(text || response.statusText)
      debugOutput = JSON.stringify(JSON.parse(text), null, 2)
    } catch (error) {
      debugOutput = String(error instanceof Error ? error.message : error)
    }
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
      <h2 id="debugger-heading">Belief Debugger</h2>
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
        <pre>{debugOutput}</pre>
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
