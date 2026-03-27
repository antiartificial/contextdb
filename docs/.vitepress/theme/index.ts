import DefaultTheme from 'vitepress/theme'
import './custom.css'
import TemporalExplorer from '../components/TemporalExplorer.vue'
import ScoringFunnel from '../components/ScoringFunnel.vue'
import CredibilityEvolution from '../components/CredibilityEvolution.vue'
import KnowledgeGaps from '../components/KnowledgeGaps.vue'
import VersionHistory from '../components/VersionHistory.vue'
import RecencyDecay from '../components/RecencyDecay.vue'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('TemporalExplorer', TemporalExplorer)
    app.component('ScoringFunnel', ScoringFunnel)
    app.component('CredibilityEvolution', CredibilityEvolution)
    app.component('KnowledgeGaps', KnowledgeGaps)
    app.component('VersionHistory', VersionHistory)
    app.component('RecencyDecay', RecencyDecay)
  }
}
