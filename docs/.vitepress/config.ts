import { defineConfig } from 'vitepress'
import { withMermaid } from 'vitepress-plugin-mermaid'

export default withMermaid(
  defineConfig({
    title: 'contextdb',
    description: 'The epistemics layer for AI systems',
    base: '/contextdb/',

    head: [
      ['link', { rel: 'stylesheet', href: 'https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.5.1/css/all.min.css', crossorigin: 'anonymous' }],
      ['script', { src: 'https://kit.fontawesome.com/501e400daa.js', crossorigin: 'anonymous' }],
    ],

    themeConfig: {
      logo: undefined,

      nav: [
        { text: 'Quick Start', link: '/quick-start' },
        { text: 'Examples', link: '/examples' },
        { text: 'API', link: '/api/' },
        { text: 'GitHub', link: 'https://github.com/antiartificial/contextdb' },
      ],

      sidebar: {
        '/': [
          {
            text: 'Getting Started',
            items: [
              { text: 'Quick Start', link: '/quick-start' },
              { text: 'Examples', link: '/examples' },
            ]
          },
          {
            text: 'Concepts',
            collapsed: false,
            items: [
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
              { text: 'Kubernetes', link: '/deployment/kubernetes' },
              { text: 'Scaled Deployment', link: '/deployment/scaled' },
            ]
          },
          {
            text: 'Benchmarks',
            link: '/benchmarks',
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
  })
)
