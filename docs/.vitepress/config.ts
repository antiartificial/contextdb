import { defineConfig } from 'vitepress'
import { withMermaid } from 'vitepress-plugin-mermaid'

export default withMermaid(
  defineConfig({
    title: 'contextdb',
    description: 'The epistemics layer for AI systems',
    base: '/contextdb/',

    head: [
      ['link', { rel: 'stylesheet', href: 'https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.5.1/css/all.min.css', integrity: 'sha512-DTOQO9RWCH3ppGqcWaEA1BIZOC6xxalwEsw9c2QQeAIftl+Vegovlnee1c9QX4TctnWMn13TZye+giMm8e2LwA==', crossorigin: 'anonymous' }],
    ],

    themeConfig: {
      logo: undefined,

      nav: [
        { text: 'Quick Start', link: '/quick-start' },
        { text: 'Examples', link: '/examples' },
        { text: 'API', link: '/api/' },
        { text: 'Releases', link: '/releases/' },
        { text: 'GitHub', link: 'https://github.com/antiartificial/contextdb' },
      ],

      sidebar: {
        '/': [
          {
            text: 'Getting Started',
            items: [
              { text: 'Quick Start', link: '/quick-start' },
              { text: 'Examples', link: '/examples' },
              { text: 'Releases', link: '/releases/' },
              { text: 'Release Health', link: '/release-health' },
              { text: 'v0.70.0 Recap', link: '/releases/v0.70.0' },
              { text: 'v0.69.0 Recap', link: '/releases/v0.69.0' },
              { text: 'v0.68.0 Recap', link: '/releases/v0.68.0' },
              { text: 'v0.67.0 Recap', link: '/releases/v0.67.0' },
              { text: 'v0.66.0 Recap', link: '/releases/v0.66.0' },
              { text: 'v0.65.0 Recap', link: '/releases/v0.65.0' },
              { text: 'v0.64.0 Recap', link: '/releases/v0.64.0' },
              { text: 'v0.63.0 Recap', link: '/releases/v0.63.0' },
              { text: 'v0.62.0 Recap', link: '/releases/v0.62.0' },
              { text: 'v0.61.0 Recap', link: '/releases/v0.61.0' },
              { text: 'v0.60.0 Recap', link: '/releases/v0.60.0' },
              { text: 'v0.59.0 Recap', link: '/releases/v0.59.0' },
              { text: 'v0.58.0 Recap', link: '/releases/v0.58.0' },
              { text: 'v0.57.0 Recap', link: '/releases/v0.57.0' },
              { text: 'v0.56.0 Recap', link: '/releases/v0.56.0' },
              { text: 'v0.55.0 Recap', link: '/releases/v0.55.0' },
              { text: 'v0.54.0 Recap', link: '/releases/v0.54.0' },
              { text: 'v0.53.0 Recap', link: '/releases/v0.53.0' },
              { text: 'v0.52.0 Recap', link: '/releases/v0.52.0' },
              { text: 'v0.51.0 Recap', link: '/releases/v0.51.0' },
              { text: 'v0.50.0 Recap', link: '/releases/v0.50.0' },
              { text: 'v0.49.0 Recap', link: '/releases/v0.49.0' },
              { text: 'v0.48.0 Recap', link: '/releases/v0.48.0' },
              { text: 'v0.47.0 Recap', link: '/releases/v0.47.0' },
              { text: 'v0.46.0 Recap', link: '/releases/v0.46.0' },
              { text: 'v0.45.0 Recap', link: '/releases/v0.45.0' },
              { text: 'v0.44.0 Recap', link: '/releases/v0.44.0' },
              { text: 'v0.43.0 Recap', link: '/releases/v0.43.0' },
              { text: 'v0.42.0 Recap', link: '/releases/v0.42.0' },
              { text: 'v0.41.0 Recap', link: '/releases/v0.41.0' },
              { text: 'v0.40.0 Recap', link: '/releases/v0.40.0' },
              { text: 'v0.39.0 Recap', link: '/releases/v0.39.0' },
              { text: 'v0.38.0 Recap', link: '/releases/v0.38.0' },
              { text: 'v0.37.0 Recap', link: '/releases/v0.37.0' },
              { text: 'v0.36.0 Recap', link: '/releases/v0.36.0' },
              { text: 'v0.35.0 Recap', link: '/releases/v0.35.0' },
              { text: 'v0.34.0 Recap', link: '/releases/v0.34.0' },
              { text: 'v0.33.0 Recap', link: '/releases/v0.33.0' },
              { text: 'v0.32.0 Recap', link: '/releases/v0.32.0' },
              { text: 'v0.31.0 Recap', link: '/releases/v0.31.0' },
              { text: 'v0.30.0 Recap', link: '/releases/v0.30.0' },
              { text: 'v0.29.0 Recap', link: '/releases/v0.29.0' },
              { text: 'v0.28.0 Recap', link: '/releases/v0.28.0' },
              { text: 'v0.27.0 Recap', link: '/releases/v0.27.0' },
              { text: 'v0.26.0 Recap', link: '/releases/v0.26.0' },
              { text: 'v0.25.0 Recap', link: '/releases/v0.25.0' },
              { text: 'v0.24.0 Recap', link: '/releases/v0.24.0' },
              { text: 'v0.23.0 Recap', link: '/releases/v0.23.0' },
              { text: 'v0.22.0 Recap', link: '/releases/v0.22.0' },
              { text: 'v0.21.0 Recap', link: '/releases/v0.21.0' },
              { text: 'v0.20.0 Recap', link: '/releases/v0.20.0' },
              { text: 'v0.19.0 Recap', link: '/releases/v0.19.0' },
              { text: 'v0.18.0 Recap', link: '/releases/v0.18.0' },
              { text: 'v0.17.0 Recap', link: '/releases/v0.17.0' },
              { text: 'v0.16.0 Recap', link: '/releases/v0.16.0' },
              { text: 'v0.15.0 Recap', link: '/releases/v0.15.0' },
              { text: 'v0.14.0 Recap', link: '/releases/v0.14.0' },
              { text: 'v0.13.0 Recap', link: '/releases/v0.13.0' },
              { text: 'v0.12.0 Recap', link: '/releases/v0.12.0' },
              { text: 'v0.11.2 Recap', link: '/releases/v0.11.2' },
              { text: 'v0.11.1 Recap', link: '/releases/v0.11.1' },
              { text: 'v0.11.0 Recap', link: '/releases/v0.11.0' },
              { text: 'v0.10.0 Recap', link: '/releases/v0.10.0' },
              { text: 'v0.9.0 Recap', link: '/releases/v0.9.0' },
              { text: 'v0.8.0 Recap', link: '/releases/v0.8.0' },
              { text: 'v0.7.0 Recap', link: '/releases/v0.7.0' },
              { text: 'v0.6.0 Recap', link: '/releases/v0.6.0' },
              { text: 'v0.5.0 Recap', link: '/releases/v0.5.0' },
              { text: 'v0.4.1 Recap', link: '/releases/v0.4.1' },
              { text: 'v0.4.0 Recap', link: '/releases/v0.4.0' },
              { text: 'v0.3.0 Recap', link: '/releases/v0.3.0' },
            ]
          },
          {
            text: 'Concepts',
            collapsed: false,
            items: [
              { text: 'Data Modeling', link: '/concepts/data-modeling' },
              { text: 'Scoring Function', link: '/concepts/scoring' },
              { text: 'Bi-Temporal Model', link: '/concepts/temporal' },
              { text: 'Source Credibility', link: '/concepts/credibility' },
              { text: 'Namespace Modes', link: '/concepts/namespaces' },
              { text: 'Memory Types', link: '/concepts/memory-types' },
              { text: 'Conflict Detection', link: '/concepts/conflict-detection' },
              { text: 'RBAC', link: '/concepts/rbac' },
              { text: 'Active Recall (SM-2)', link: '/concepts/sm2' },
              { text: 'Epistemics Layer', link: '/concepts/epistemics' },
            ]
          },
          {
            text: 'Architecture',
            collapsed: false,
            items: [
              { text: 'System Overview', link: '/architecture/overview' },
              { text: 'Write Path', link: '/architecture/write-path' },
              { text: 'Read Path', link: '/architecture/read-path' },
              { text: 'Auto-Embedding', link: '/architecture/embedding' },
              { text: 'Background Workers', link: '/architecture/background-workers' },
            ]
          },
          {
            text: 'API Reference',
            collapsed: false,
            items: [
              { text: 'Go SDK', link: '/api/go-sdk' },
              { text: 'gRPC API', link: '/api/grpc' },
              { text: 'REST API', link: '/api/rest' },
              { text: 'GraphQL API', link: '/api/graphql' },
              { text: 'Python SDK', link: '/api/python-sdk' },
              { text: 'TypeScript SDK', link: '/api/typescript-sdk' },
              { text: 'Query DSL', link: '/api/dsl' },
            ]
          },
          {
            text: 'Deployment',
            collapsed: true,
            items: [
              { text: 'Embedded Mode', link: '/deployment/embedded' },
              { text: 'Docker', link: '/deployment/docker' },
              { text: 'Operations', link: '/deployment/operations' },
              { text: 'Backup Runbook', link: '/deployment/backup-runbook' },
              { text: 'Published Backup Repair Guard', link: '/deployment/published-backup-repair-guard' },
              { text: 'KV Derivation Recipes', link: '/deployment/kv-derivation-recipes' },
              { text: 'Retry Fatigue Cookbook', link: '/deployment/retry-fatigue-cookbook' },
              { text: 'Mini/Norn', link: '/deployment/norn' },
              { text: 'Kubernetes', link: '/deployment/kubernetes' },
              { text: 'Scaled Deployment', link: '/deployment/scaled' },
            ]
          },
          {
            text: 'Benchmarks',
            link: '/benchmarks',
          },
          {
            text: 'Release Health',
            link: '/release-health',
          },
          {
            text: 'Feature Matrix',
            link: '/feature-matrix',
          },
        ],
      },

      search: {
        provider: 'local',
      },

      socialLinks: [
        { icon: 'github', link: 'https://github.com/antiartificial/contextdb' },
      ],

      footer: {
        message: 'Released under the MIT License.',
        copyright: 'Copyright 2025 Antiartificial',
      },

      outline: {
        level: [2, 3],
      },
    },

    markdown: {
      lineNumbers: true,
    },

    mermaid: {
      theme: 'dark',
    },

    mermaidPlugin: {
      class: 'mermaid',
    },

    vite: {
      build: {
        chunkSizeWarningLimit: 1500,
      },
    },
  })
)
