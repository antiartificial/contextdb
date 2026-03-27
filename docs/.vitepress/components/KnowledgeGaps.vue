<script setup>
import { ref, computed, onMounted } from 'vue'
import { animate } from 'motion'

const topics = [
  { name: "Go GC",          x: 20, y: 30, density: 0.9,  confidence: 0.85, nodes: 24 },
  { name: "Go concurrency", x: 60, y: 25, density: 0.85, confidence: 0.90, nodes: 18 },
  { name: "Go networking",  x: 70, y: 65, density: 0.6,  confidence: 0.75, nodes: 8  },
  { name: "Go testing",     x: 25, y: 70, density: 0.7,  confidence: 0.80, nodes: 12 },
  { name: "Go performance", x: 45, y: 50, density: 0.4,  confidence: 0.60, nodes: 5  },
]

const gaps = [
  { name: "Memory allocation?",  x: 40, y: 30, between: ["Go GC",          "Go concurrency"] },
  { name: "HTTP/2 internals?",   x: 65, y: 45, between: ["Go concurrency",  "Go networking"]  },
  { name: "Benchmark patterns?", x: 35, y: 60, between: ["Go testing",      "Go performance"] },
]

// Coverage = average confidence weighted by node count
const coverageScore = computed(() => {
  const totalNodes = topics.reduce((s, t) => s + t.nodes, 0)
  const weighted = topics.reduce((s, t) => s + t.confidence * t.nodes, 0)
  return Math.round((weighted / totalNodes) * 100)
})

const hoveredGap = ref(null)
const criticalGap = ref(null)
const gapRefs = {}

function setGapRef(name, el) {
  if (el) gapRefs[name] = el
}

function topicRadius(t) {
  // 5–22px based on node count (max 24 nodes)
  return 5 + (t.nodes / 24) * 17
}

function topicOpacity(t) {
  return 0.45 + t.confidence * 0.55
}

// Lines from each gap to its two neighboring topics
const connections = computed(() => {
  const lines = []
  gaps.forEach(gap => {
    gap.between.forEach(tName => {
      const t = topics.find(x => x.name === tName)
      if (t) lines.push({ x1: gap.x, y1: gap.y, x2: t.x, y2: t.y, gap: gap.name })
    })
  })
  return lines
})

function suggestCritical() {
  // Most critical = gap between the two highest-confidence topics
  const scored = gaps.map(g => {
    const score = g.between.reduce((s, n) => {
      const t = topics.find(x => x.name === n)
      return s + (t ? t.confidence : 0)
    }, 0)
    return { ...g, score }
  })
  scored.sort((a, b) => b.score - a.score)
  const top = scored[0]
  criticalGap.value = top.name
  const el = gapRefs[top.name]
  if (el) {
    animate(el, { scale: [1, 1.5, 1, 1.4, 1] }, { duration: 0.8, easing: 'ease-in-out' })
  }
  setTimeout(() => { criticalGap.value = null }, 2000)
}

const tooltipGap = computed(() => gaps.find(g => g.name === hoveredGap.value))
</script>

<template>
  <div class="kg-wrap">
    <!-- Coverage meter -->
    <div class="kg-header">
      <span class="kg-title">Semantic Coverage Map</span>
      <div class="kg-coverage">
        <span class="kg-coverage-label">Coverage</span>
        <div class="kg-coverage-bar">
          <div class="kg-coverage-fill" :style="{ width: coverageScore + '%' }"></div>
        </div>
        <span class="kg-coverage-pct">{{ coverageScore }}%</span>
      </div>
      <button class="kg-btn" @click="suggestCritical">Suggest</button>
    </div>

    <!-- Map area -->
    <div class="kg-map">
      <!-- SVG connection lines -->
      <svg class="kg-svg" viewBox="0 0 100 100" preserveAspectRatio="none">
        <line
          v-for="(c, i) in connections"
          :key="i"
          :x1="c.x1 + '%'" :y1="c.y1 + '%'"
          :x2="c.x2 + '%'" :y2="c.y2 + '%'"
          class="kg-line"
          :class="{ 'kg-line--active': hoveredGap === c.gap || criticalGap === c.gap }"
        />
      </svg>

      <!-- Known topics -->
      <div
        v-for="t in topics"
        :key="t.name"
        class="kg-topic"
        :style="{
          left: t.x + '%',
          top: t.y + '%',
          width: topicRadius(t) * 2 + 'px',
          height: topicRadius(t) * 2 + 'px',
          opacity: topicOpacity(t),
        }"
        :title="t.name + ' — ' + t.nodes + ' nodes, ' + Math.round(t.confidence * 100) + '% confidence'"
      >
        <span class="kg-topic-label">{{ t.name }}</span>
      </div>

      <!-- Gaps -->
      <div
        v-for="g in gaps"
        :key="g.name"
        :ref="el => setGapRef(g.name, el)"
        class="kg-gap"
        :class="{ 'kg-gap--highlighted': criticalGap === g.name }"
        :style="{ left: g.x + '%', top: g.y + '%' }"
        @mouseenter="hoveredGap = g.name"
        @mouseleave="hoveredGap = null"
      >
        <span class="kg-gap-label">?</span>

        <!-- Tooltip -->
        <div v-if="hoveredGap === g.name" class="kg-tooltip">
          Gap between <strong>{{ g.between[0] }}</strong> and <strong>{{ g.between[1] }}</strong>.
          Consider adding information about <em>{{ g.name.replace('?', '') }}</em>.
        </div>
      </div>
    </div>

    <!-- Legend -->
    <div class="kg-legend">
      <span class="kg-legend-item">
        <span class="kg-legend-dot kg-legend-dot--topic"></span>Known topic (size = node count)
      </span>
      <span class="kg-legend-item">
        <span class="kg-legend-dot kg-legend-dot--gap"></span>Knowledge gap
      </span>
    </div>
  </div>
</template>

<style scoped>
.kg-wrap {
  max-width: 700px;
  margin: 1.5rem 0;
  padding: 1rem 1.25rem;
  border: 1px solid var(--vp-c-divider);
  border-radius: 10px;
  background: var(--vp-c-bg-soft);
  font-size: 0.82rem;
}

.kg-header {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 0.9rem;
  flex-wrap: wrap;
}
.kg-title {
  font-weight: 600;
  color: var(--vp-c-text-1);
  font-size: 0.9rem;
  flex: 1;
}
.kg-coverage {
  display: flex;
  align-items: center;
  gap: 0.4rem;
}
.kg-coverage-label {
  color: var(--vp-c-text-2);
  font-size: 0.78rem;
}
.kg-coverage-bar {
  width: 80px;
  height: 6px;
  border-radius: 3px;
  background: var(--vp-c-divider);
  overflow: hidden;
}
.kg-coverage-fill {
  height: 100%;
  border-radius: 3px;
  background: var(--vp-c-brand-1);
  transition: width 0.6s ease;
}
.kg-coverage-pct {
  font-weight: 700;
  color: var(--vp-c-brand-1);
  font-size: 0.85rem;
  min-width: 2.5rem;
}
.kg-btn {
  padding: 0.2rem 0.75rem;
  border-radius: 5px;
  border: 1px solid var(--vp-c-brand-1);
  background: var(--vp-c-brand-soft);
  color: var(--vp-c-brand-1);
  cursor: pointer;
  font-size: 0.78rem;
  font-weight: 600;
  transition: background 0.15s, color 0.15s;
}
.kg-btn:hover {
  background: var(--vp-c-brand-1);
  color: #fff;
}

/* Map */
.kg-map {
  position: relative;
  width: 100%;
  height: 260px;
  border: 1px solid var(--vp-c-divider);
  border-radius: 8px;
  background: var(--vp-c-bg);
  overflow: hidden;
  margin-bottom: 0.75rem;
}

.kg-svg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
}
.kg-line {
  stroke: var(--vp-c-divider);
  stroke-width: 0.5;
  stroke-dasharray: 2 2;
  opacity: 0.5;
  transition: stroke 0.2s, opacity 0.2s;
}
.kg-line--active {
  stroke: #f97316;
  opacity: 0.8;
}

/* Known topics */
.kg-topic {
  position: absolute;
  border-radius: 50%;
  background: radial-gradient(circle at 35% 35%, var(--vp-c-brand-1), #2563eb);
  transform: translate(-50%, -50%);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: default;
  transition: transform 0.2s;
}
.kg-topic:hover {
  transform: translate(-50%, -50%) scale(1.12);
  z-index: 2;
}
.kg-topic-label {
  position: absolute;
  top: calc(100% + 4px);
  left: 50%;
  transform: translateX(-50%);
  white-space: nowrap;
  font-size: 0.65rem;
  color: var(--vp-c-text-2);
  pointer-events: none;
}

/* Gaps */
.kg-gap {
  position: absolute;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 2px dashed #f97316;
  transform: translate(-50%, -50%);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  animation: kg-pulse 2.4s ease-in-out infinite;
  z-index: 3;
  transition: border-color 0.2s;
}
.kg-gap--highlighted {
  border-color: #ef4444;
  box-shadow: 0 0 12px rgba(239, 68, 68, 0.5);
}
@keyframes kg-pulse {
  0%, 100% { box-shadow: 0 0 0 0 rgba(249, 115, 22, 0.3); }
  50%       { box-shadow: 0 0 0 6px rgba(249, 115, 22, 0); }
}
.kg-gap-label {
  font-size: 0.75rem;
  font-weight: 700;
  color: #f97316;
  line-height: 1;
}

/* Tooltip */
.kg-tooltip {
  position: absolute;
  bottom: calc(100% + 8px);
  left: 50%;
  transform: translateX(-50%);
  background: var(--vp-c-bg-elv, var(--vp-c-bg-soft));
  border: 1px solid var(--vp-c-divider);
  border-radius: 6px;
  padding: 0.4rem 0.6rem;
  font-size: 0.72rem;
  color: var(--vp-c-text-1);
  white-space: nowrap;
  z-index: 10;
  pointer-events: none;
  box-shadow: 0 2px 8px rgba(0,0,0,0.15);
}

/* Legend */
.kg-legend {
  display: flex;
  gap: 1.25rem;
  flex-wrap: wrap;
  color: var(--vp-c-text-3);
  font-size: 0.75rem;
}
.kg-legend-item {
  display: flex;
  align-items: center;
  gap: 0.35rem;
}
.kg-legend-dot {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 50%;
}
.kg-legend-dot--topic {
  background: var(--vp-c-brand-1);
}
.kg-legend-dot--gap {
  background: transparent;
  border: 2px dashed #f97316;
}
</style>
