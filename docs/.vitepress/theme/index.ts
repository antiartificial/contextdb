import DefaultTheme from 'vitepress/theme'
import './custom.css'
import TemporalExplorer from '../components/TemporalExplorer.vue'
import ScoringFunnel from '../components/ScoringFunnel.vue'
import CredibilityEvolution from '../components/CredibilityEvolution.vue'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('TemporalExplorer', TemporalExplorer)
    app.component('ScoringFunnel', ScoringFunnel)
    app.component('CredibilityEvolution', CredibilityEvolution)
  }
}
