<script setup>
import { ref, computed, nextTick, onMounted } from 'vue'
import { animate } from 'motion'

const candidates = [
  { id: 0, text: 'Go uses garbage collection',  sim: 0.95, conf: 0.90, rec: 0.80, util: 0.50 },
  { id: 1, text: 'Go GC uses mark-and-sweep',   sim: 0.88, conf: 0.95, rec: 0.30, util: 0.90 },
  { id: 2, text: 'Python uses ref counting',    sim: 0.70, conf: 0.80, rec: 0.95, util: 0.20 },
  { id: 3, text: 'Java GC is generational',     sim: 0.60, conf: 0.85, rec: 0.60, util: 0.10 },
  { id: 4, text: 'Rust has no GC',              sim: 0.55, conf: 0.99, rec: 0.40, util: 0.30 },
  { id: 5, text: 'Go GC pauses under 1ms',      sim: 0.82, conf: 0.45, rec: 0.10, util: 0.70 },
]

const presets = {
  General:          { sim: 40, conf: 30, rec: 20, util: 10 },
  'Belief System':  { sim: 30, conf: 45, rec: 20, util:  5 },
  'Agent Memory':   { sim: 35, conf: 20, rec: 25, util: 20 },
  Procedural:       { sim: 40, conf: 40, rec: 15, util:  5 },
}

const wSim  = ref(40)
const wConf = ref(30)
const wRec  = ref(20)
const wUtil = ref(10)
const activePreset = ref('General')

function applyPreset(name) {
  const p = presets[name]
  wSim.value  = p.sim
  wConf.value = p.conf
  wRec.value  = p.rec
  wUtil.value = p.util
  activePreset.value = name
  refreshBars()
}

function normalise(w1, w2, w3, w4) {
  const t = w1 + w2 + w3 + w4 || 1
  return [w1/t, w2/t, w3/t, w4/t]
}

function computeScore(c) {
  const [ns, nc, nr, nu] = normalise(wSim.value, wConf.value, wRec.value, wUtil.value)
  return ns * c.sim + nc * c.conf + nr * c.rec + nu * c.util
}

const sortedCandidates = computed(() => {
  return [...candidates]
    .map(c => ({ ...c, score: computeScore(c) }))
    .sort((a, b) => b.score - a.score)
})

// widths as % of container (max score mapped to ~92%)
function widthPct(c) {
  const scores = candidates.map(x => computeScore(x))
  const max = Math.max(...scores)
  return max > 0 ? (computeScore(c) / max) * 92 : 0
}

function segWidths(c) {
  const [ns, nc, nr, nu] = normalise(wSim.value, wConf.value, wRec.value, wUtil.value)
  const total = computeScore(c)
  const max = Math.max(...candidates.map(x => computeScore(x)))
  const scale = max > 0 ? 92 / max : 0
  return {
    sim:  (ns * c.sim)  * scale,
    conf: (nc * c.conf) * scale,
    rec:  (nr * c.rec)  * scale,
    util: (nu * c.util) * scale,
  }
}

const barRefs = {}
function setBarRef(id, el) {
  if (el) barRefs[id] = el
}

function refreshBars() {
  nextTick(() => {
    candidates.forEach(c => {
      const el = barRefs[c.id]
      if (!el) return
      const w = widthPct(c)
      animate(el, { width: `${w}%` }, { duration: 0.4, easing: [0.4, 0, 0.2, 1] })
      // animate segments
      const segs = el.querySelectorAll('.seg')
      const sw = segWidths(c)
      const keys = ['sim', 'conf', 'rec', 'util']
      segs.forEach((s, i) => {
        animate(s, { width: `${sw[keys[i]]}%` }, { duration: 0.4, easing: [0.4, 0, 0.2, 1] })
      })
    })
  })
}

onMounted(() => refreshBars())

function onSliderChange() {
  activePreset.value = ''
  refreshBars()
}
</script>

<template>
  <div class="sf-wrap">
    <div class="sf-presets">
      <span class="sf-label">Preset:</span>
      <button v-for="(_, name) in presets" :key="name" class="sf-btn"
        :class="{ active: activePreset === name }" @click="applyPreset(name)">{{ name }}</button>
    </div>
    <div class="sf-sliders">
      <div v-for="(dim, key) in { Similarity: wSim, Confidence: wConf, Recency: wRec, Utility: wUtil }" :key="key" class="sf-slider-row">
        <span class="sf-dim-label">{{ dim }}</span>
        <span class="sf-dim-name">{{ key }}</span>
        <input type="range" min="0" max="100" step="1" :value="dim"
          :class="['sf-range', key.toLowerCase()]"
          @input="e => { if(key==='Similarity') wSim=+e.target.value; else if(key==='Confidence') wConf=+e.target.value; else if(key==='Recency') wRec=+e.target.value; else wUtil=+e.target.value; onSliderChange() }" />
      </div>
    </div>
    <div class="sf-bars">
      <transition-group name="sf-list" tag="div" class="sf-list">
        <div v-for="c in sortedCandidates" :key="c.id" class="sf-row">
          <span class="sf-text">{{ c.text }}</span>
          <div class="sf-track">
            <div class="sf-bar" :ref="el => setBarRef(c.id, el)" :style="{ width: widthPct(c) + '%' }">
              <div v-for="seg in ['sim','conf','rec','util']" :key="seg"
                   class="seg" :class="seg" :style="{ width: segWidths(c)[seg] + '%' }"></div>
              <span class="sf-score">{{ c.score.toFixed(2) }}</span>
            </div>
          </div>
        </div>
      </transition-group>
    </div>
    <div class="sf-legend">
      <span class="sf-dot sim"></span>Similarity
      <span class="sf-dot conf"></span>Confidence
      <span class="sf-dot rec"></span>Recency
      <span class="sf-dot util"></span>Utility
    </div>
  </div>
</template>

<style scoped>
.sf-wrap {
  max-width: 700px;
  margin: 1.5rem 0;
  padding: 1rem 1.25rem;
  border: 1px solid var(--vp-c-divider);
  border-radius: 10px;
  background: var(--vp-c-bg-soft);
  font-size: 0.82rem;
}

/* presets */
.sf-presets {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
  margin-bottom: 0.9rem;
}
.sf-label { color: var(--vp-c-text-2); font-size: 0.78rem; }
.sf-btn {
  padding: 0.2rem 0.65rem;
  border-radius: 5px;
  border: 1px solid var(--vp-c-divider);
  background: var(--vp-c-bg);
  color: var(--vp-c-text-2);
  cursor: pointer;
  font-size: 0.78rem;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}
.sf-btn:hover { border-color: var(--vp-c-brand); color: var(--vp-c-brand); }
.sf-btn.active { background: var(--vp-c-brand-soft); border-color: var(--vp-c-brand); color: var(--vp-c-brand); }

.sf-sliders { margin-bottom: 1rem; display: flex; flex-direction: column; gap: 0.4rem; }
.sf-slider-row { display: flex; align-items: center; gap: 0.5rem; }
.sf-dim-label { width: 2rem; text-align: right; font-variant-numeric: tabular-nums; color: var(--vp-c-text-1); font-weight: 600; }
.sf-dim-name  { width: 5.5rem; color: var(--vp-c-text-2); }
.sf-range {
  flex: 1;
  -webkit-appearance: none;
  appearance: none;
  width: 100%;
  height: 6px;
  border-radius: 3px;
  background: var(--vp-c-divider);
  outline: none;
  cursor: pointer;
}
.sf-range.similarity { accent-color: #3b82f6; }
.sf-range.confidence { accent-color: #22c55e; }
.sf-range.recency    { accent-color: #f97316; }
.sf-range.utility    { accent-color: #a855f7; }

.sf-range::-webkit-slider-track {
  height: 6px;
  border-radius: 3px;
  background: var(--vp-c-divider);
  border: 1px solid rgba(255, 255, 255, 0.1);
}
.sf-range::-moz-range-track {
  height: 6px;
  border-radius: 3px;
  background: var(--vp-c-divider);
  border: 1px solid rgba(255, 255, 255, 0.1);
}
.sf-range::-webkit-slider-thumb {
  -webkit-appearance: none;
  appearance: none;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--vp-c-brand-1);
  cursor: pointer;
  border: 2px solid var(--vp-c-bg);
  box-shadow: 0 1px 4px rgba(0,0,0,0.3);
}
.sf-range::-moz-range-thumb {
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--vp-c-brand-1);
  cursor: pointer;
  border: 2px solid var(--vp-c-bg);
  box-shadow: 0 1px 4px rgba(0,0,0,0.3);
}
.sf-bars { margin-bottom: 0.75rem; }
.sf-list { display: flex; flex-direction: column; gap: 0.4rem; }
.sf-row { display: flex; align-items: center; gap: 0.5rem; }
.sf-text { width: 11rem; min-width: 11rem; color: var(--vp-c-text-2); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-size: 0.78rem; }
.sf-track { flex: 1; height: 20px; background: var(--vp-c-bg); border-radius: 10px; position: relative; }
.sf-bar { height: 20px; border-radius: 10px; display: flex; overflow: hidden; position: relative; min-width: 2rem; }
.seg { height: 100%; flex-shrink: 0; }
.seg.sim  { background: #3b82f6; }
.seg.conf { background: #22c55e; }
.seg.rec  { background: #f97316; }
.seg.util { background: #a855f7; }
.sf-score { position: absolute; right: 6px; top: 50%; transform: translateY(-50%); color: #fff; font-size: 0.72rem; font-weight: 700; text-shadow: 0 1px 2px rgba(0,0,0,0.5); pointer-events: none; }
.sf-list-move { transition: transform 0.4s cubic-bezier(0.4,0,0.2,1); }
.sf-legend { display: flex; align-items: center; gap: 0.75rem; color: var(--vp-c-text-3); font-size: 0.75rem; flex-wrap: wrap; }
.sf-dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%; margin-right: 3px; }
.sf-dot.sim  { background: #3b82f6; }
.sf-dot.conf { background: #22c55e; }
.sf-dot.rec  { background: #f97316; }
.sf-dot.util { background: #a855f7; }
</style>
