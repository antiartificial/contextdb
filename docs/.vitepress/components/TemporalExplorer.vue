<template>
  <div class="te-root">
    <div class="te-header">
      <h3 class="te-title">Bi-Temporal Explorer</h3>
      <p class="te-subtitle">Drag the slider to travel through time and see what the system knew vs. what was actually true.</p>
    </div>

    <!-- Legend -->
    <div class="te-legend">
      <span class="te-dot te-dot--valid"></span><span>Valid Time (world truth)</span>
      <span class="te-dot te-dot--tx" style="margin-left:1rem"></span><span>Transaction Time (system learned)</span>
      <span class="te-dot te-dot--retracted" style="margin-left:1rem"></span><span>Retracted</span>
    </div>

    <!-- Timeline ruler -->
    <div class="te-timeline">
      <div class="te-months">
        <span v-for="m in months" :key="m">{{ m }}</span>
      </div>

      <!-- Valid-time track -->
      <div class="te-track te-track--valid">
        <span class="te-track-label">Valid Time</span>
        <div class="te-bars-row">
          <template v-for="fact in facts" :key="fact.id + '-v'">
            <div
              class="te-bar te-bar--valid"
              :style="barStyle(fact.validFrom, fact.validUntil)"
              :title="fact.text"
            ></div>
          </template>
        </div>
      </div>

      <!-- Transaction-time track -->
      <div class="te-track te-track--tx">
        <span class="te-track-label">Tx Time</span>
        <div class="te-bars-row">
          <template v-for="fact in facts" :key="fact.id + '-t'">
            <div
              class="te-bar te-bar--tx"
              :style="barStyle(fact.txTime, null)"
              :title="fact.text"
            ></div>
          </template>
        </div>
      </div>

      <!-- Slider -->
      <div class="te-slider-wrap">
        <input
          type="range"
          class="te-slider"
          :min="0"
          :max="sliderMax"
          v-model.number="sliderVal"
        />
        <div class="te-needle" :style="{ left: needleLeft }"></div>
        <div class="te-needle-label" :style="{ left: needleLeft }">{{ currentLabel }}</div>
      </div>
    </div>

    <!-- Current View panel -->
    <div class="te-panel">
      <div class="te-panel-col">
        <div class="te-panel-head">What was true at <strong>{{ currentLabel }}</strong></div>
        <TransitionGroup name="fact-list" tag="div">
          <div
            v-for="fact in worldView"
            :key="fact.id"
            class="te-fact"
            :class="['te-fact--' + fact.state]"
          >
            <span class="te-fact-dot"></span>
            <div>
              <div class="te-fact-text">{{ fact.text }}</div>
              <div class="te-fact-meta">valid {{ fmt(fact.validFrom) }}{{ fact.validUntil ? ' → ' + fmt(fact.validUntil) : '' }}{{ fact.retracted && fact.retractReason ? ' · retracted: ' + fact.retractReason : '' }}</div>
            </div>
          </div>
          <div v-if="worldView.length === 0" key="empty" class="te-empty">No facts valid at this time.</div>
        </TransitionGroup>
      </div>

      <div class="te-panel-col">
        <div class="te-panel-head">What the system knew at <strong>{{ currentLabel }}</strong></div>
        <TransitionGroup name="fact-list" tag="div">
          <div
            v-for="fact in systemView"
            :key="fact.id"
            class="te-fact"
            :class="['te-fact--' + fact.state, fact.notYetKnown ? 'te-fact--dim' : '']"
          >
            <span class="te-fact-dot"></span>
            <div>
              <div class="te-fact-text">{{ fact.text }}
                <span v-if="fact.notYetKnown" class="te-badge">not yet discovered</span>
              </div>
              <div class="te-fact-meta">system learned {{ fmt(fact.txTime) }}</div>
            </div>
          </div>
          <div v-if="systemView.length === 0" key="empty2" class="te-empty">System had no knowledge at this time.</div>
        </TransitionGroup>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { animate } from 'motion'

// ── Data ────────────────────────────────────────────────────────────────────
const START = new Date('2025-01-01').getTime()
const END   = new Date('2025-07-01').getTime()
const RANGE = END - START
const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun']
const sliderMax = 180  // days

const facts = [
  {
    id: 'api-100',
    text: 'API limit: 100 req/s',
    validFrom:  new Date('2025-01-01'),
    validUntil: new Date('2025-03-15'),
    txTime:     new Date('2025-01-05'),
    retracted:  false,
  },
  {
    id: 'api-500',
    text: 'API limit: 500 req/s',
    validFrom:  new Date('2025-03-15'),
    validUntil: null,
    txTime:     new Date('2025-03-20'),
    retracted:  false,
  },
  {
    id: 'team-5',
    text: 'Team size: 5',
    validFrom:  new Date('2025-02-01'),
    validUntil: new Date('2025-04-01'),
    txTime:     new Date('2025-02-01'),
    retracted:  false,
  },
  {
    id: 'team-8',
    text: 'Team size: 8',
    validFrom:  new Date('2025-04-01'),
    validUntil: null,
    txTime:     new Date('2025-04-01'),
    retracted:  false,
  },
  {
    id: 'office-a',
    text: 'Office location: Building A',
    validFrom:  new Date('2025-01-01'),
    validUntil: new Date('2025-05-01'),
    txTime:     new Date('2025-01-01'),
    retracted:  true,
    retractedAt: new Date('2025-03-15'),
    retractReason: 'incorrect — building was B',
  },
]

// ── Slider state ─────────────────────────────────────────────────────────────
const sliderVal = ref(0)

const currentDate = computed(() => {
  return new Date(START + (sliderVal.value / sliderMax) * RANGE)
})

const currentLabel = computed(() => fmt(currentDate.value))

const needleLeft = computed(() => {
  return (sliderVal.value / sliderMax * 100).toFixed(1) + '%'
})

function fmt(d) {
  if (!d) return ''
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

// ── Bar positioning ───────────────────────────────────────────────────────────
function pct(d) {
  return Math.max(0, Math.min(100, (d.getTime() - START) / RANGE * 100))
}

function barStyle(from, until) {
  const left  = pct(from)
  const right = until ? pct(until) : 100
  return { left: left + '%', width: Math.max(1, right - left) + '%' }
}

// ── Computed views ────────────────────────────────────────────────────────────
function factState(fact, t) {
  if (fact.retracted) return 'retracted'
  if (fact.validUntil && fact.validUntil <= t) return 'past'
  return 'active'
}

const worldView = computed(() => {
  const t = currentDate.value
  return facts
    .filter(f => {
      const after  = f.validFrom  <= t
      const before = !f.validUntil || f.validUntil > t
      return after && before
    })
    .map(f => ({ ...f, state: factState(f, t), notYetKnown: false }))
})

const systemView = computed(() => {
  const t = currentDate.value
  return facts
    .filter(f => f.txTime <= t)
    .map(f => {
      const notYetKnown = false // system already learned it by txTime
      return { ...f, state: factState(f, t), notYetKnown }
    })
})

// ── Motion One animations ─────────────────────────────────────────────────────
function animateIn(el) {
  animate(el, { opacity: [0, 1], y: [10, 0] }, { duration: 0.3 })
}

function animateOut(el, done) {
  animate(el, { opacity: [1, 0], y: [0, -10] }, { duration: 0.2 }).then(done)
}

onMounted(() => {
  sliderVal.value = 30 // start at ~Feb 1
})
</script>

<style scoped>
.te-root {
  background: var(--vp-c-bg-soft);
  border: 1px solid var(--vp-c-divider);
  border-radius: 12px;
  padding: 1.25rem 1.5rem;
  margin: 1.5rem 0;
  font-size: 0.875rem;
  color: var(--vp-c-text-1);
}
.te-title { margin: 0 0 0.25rem; font-size: 1rem; font-weight: 600; }
.te-subtitle { margin: 0 0 1rem; color: var(--vp-c-text-2, #888); font-size: 0.8rem; }

.te-legend { display: flex; align-items: center; gap: 0.4rem; margin-bottom: 1rem; flex-wrap: wrap; }
.te-dot { width: 10px; height: 10px; border-radius: 50%; display: inline-block; }
.te-dot--valid     { background: #22c55e; }
.te-dot--tx        { background: #3b82f6; }
.te-dot--retracted { background: #ef4444; }

/* Timeline */
.te-timeline { position: relative; padding-bottom: 3rem; }
.te-months {
  display: flex; justify-content: space-between;
  font-size: 0.7rem; color: var(--vp-c-text-2, #888);
  margin-bottom: 0.5rem;
}

.te-track { position: relative; height: 28px; margin-bottom: 0.5rem; display: flex; align-items: center; gap: 0.5rem; }
.te-track-label { width: 4.5rem; font-size: 0.7rem; text-align: right; flex-shrink: 0; color: var(--vp-c-text-2, #888); }
.te-bars-row { position: relative; flex: 1; height: 100%; border-radius: 4px; background: var(--vp-c-divider); overflow: visible; }
.te-bar {
  position: absolute; height: 100%; border-radius: 4px; opacity: 0.75;
  transition: opacity 0.2s;
}
.te-bar--valid { background: #22c55e; }
.te-bar--tx    { background: #3b82f6; }

/* Slider */
.te-slider-wrap { position: absolute; bottom: 0; left: 5rem; right: 0; }
.te-slider {
  width: 100%;
  -webkit-appearance: none;
  appearance: none;
  height: 6px;
  border-radius: 3px;
  background: var(--vp-c-divider);
  cursor: pointer;
  outline: none;
}
.te-slider::-webkit-slider-track {
  height: 6px;
  border-radius: 3px;
  background: var(--vp-c-divider);
  border: 1px solid rgba(255, 255, 255, 0.1);
}
.te-slider::-moz-range-track {
  height: 6px;
  border-radius: 3px;
  background: var(--vp-c-divider);
  border: 1px solid rgba(255, 255, 255, 0.1);
}
.te-slider::-webkit-slider-thumb {
  -webkit-appearance: none;
  appearance: none;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--vp-c-brand-1, #646cff);
  cursor: grab;
  border: 2px solid var(--vp-c-bg);
  box-shadow: 0 1px 4px rgba(0,0,0,0.3);
}
.te-slider::-moz-range-thumb {
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--vp-c-brand-1, #646cff);
  cursor: grab;
  border: 2px solid var(--vp-c-bg);
  box-shadow: 0 1px 4px rgba(0,0,0,0.3);
}
.te-needle {
  position: absolute; top: -4.5rem; bottom: 1.25rem;
  width: 2px; background: var(--vp-c-brand-1, #646cff);
  transform: translateX(-50%); pointer-events: none;
  opacity: 0.6;
}
.te-needle-label {
  position: absolute; top: -5rem;
  transform: translateX(-50%);
  background: var(--vp-c-brand-1, #646cff);
  color: #fff; font-size: 0.65rem; padding: 1px 5px;
  border-radius: 4px; white-space: nowrap; pointer-events: none;
}

/* Panel */
.te-panel { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin-top: 0.5rem; }
@media (max-width: 600px) { .te-panel { grid-template-columns: 1fr; } }
.te-panel-col {
  background: var(--vp-c-bg, #fff);
  border: 1px solid var(--vp-c-divider);
  border-radius: 8px; padding: 0.75rem;
  min-height: 6rem;
}
.te-panel-head { font-size: 0.75rem; margin-bottom: 0.5rem; color: var(--vp-c-text-2, #888); }

.te-fact {
  display: flex; align-items: flex-start; gap: 0.5rem;
  padding: 0.4rem 0.5rem; border-radius: 6px;
  border-left: 3px solid #22c55e;
  background: var(--vp-c-bg-soft);
  margin-bottom: 0.4rem;
  transition: opacity 0.3s;
}
.te-fact--past      { border-color: #94a3b8; }
.te-fact--retracted { border-color: #ef4444; }
.te-fact--dim       { opacity: 0.45; }
.te-fact-dot { width: 6px; height: 6px; border-radius: 50%; background: currentColor; margin-top: 0.35rem; flex-shrink: 0; }
.te-fact-text { font-weight: 500; }
.te-fact-meta { font-size: 0.7rem; color: var(--vp-c-text-2, #888); }
.te-badge {
  display: inline-block; margin-left: 0.4rem;
  font-size: 0.65rem; background: #fbbf24; color: #000;
  border-radius: 4px; padding: 0 4px; vertical-align: middle;
}
.te-empty { color: var(--vp-c-text-2, #888); font-size: 0.8rem; padding: 0.5rem; }

/* TransitionGroup */
.fact-list-enter-active { transition: all 0.3s ease; }
.fact-list-leave-active { transition: all 0.2s ease; position: absolute; width: 100%; }
.fact-list-enter-from   { opacity: 0; transform: translateY(8px); }
.fact-list-leave-to     { opacity: 0; transform: translateY(-8px); }
</style>
