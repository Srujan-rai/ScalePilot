// @ts-check
// `@type` JSDoc annotations allow editor autocompletion and type checking
// (when paired with `@ts-check`).
// There are various equivalent ways to declare your Docusaurus config.
// See: https://docusaurus.io/docs/api/docusaurus-config

import { themes as prismThemes } from 'prism-react-renderer';

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'ScalePilot',
  tagline: 'Pre-scale before the spike. Cloud-agnostic Kubernetes autoscaling with predictive forecasting, multi-cluster federation, and FinOps budget controls.',
  favicon: 'img/logo.svg',

  // Set the production url of your site here
  url: 'https://srujan-rai.github.io',
  // Set the /<baseUrl>/ pathname under which your site is served
  baseUrl: '/scalepilot/',

  // GitHub pages deployment config.
  organizationName: 'srujan-rai',
  projectName: 'scalepilot',
  deploymentBranch: 'gh-pages',
  trailingSlash: false,

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn',

  // Even if you don't use internationalization, you can use this field to set
  // useful metadata like html lang.
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  markdown: {
    mermaid: true,
  },

  themes: ['@docusaurus/theme-mermaid'],

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          sidebarPath: './sidebars.js',
          editUrl: 'https://github.com/srujan-rai/scalepilot/tree/main/documentation/',
          showLastUpdateTime: true,
          showLastUpdateAuthor: true,
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      image: 'img/scalepilot-social-card.png',

      colorMode: {
        defaultMode: 'light',
        disableSwitch: false,
        respectPrefersColorScheme: true,
      },

      announcementBar: {
        id: 'alpha_notice',
        content:
          'ScalePilot is in active development (v0.1.0-alpha). APIs may change. <a href="https://github.com/srujan-rai/scalepilot/issues">Report issues</a> or <a href="https://github.com/srujan-rai/scalepilot">star us on GitHub</a>!',
        backgroundColor: '#2563EB',
        textColor: '#ffffff',
        isCloseable: true,
      },

      navbar: {
        title: 'ScalePilot',
        logo: {
          alt: 'ScalePilot Logo',
          src: 'img/logo.svg',
          srcDark: 'img/logo.svg',
        },
        hideOnScroll: false,
        items: [
          {
            type: 'docSidebar',
            sidebarId: 'docsSidebar',
            position: 'left',
            label: 'Docs',
          },
          {
            to: '/docs/getting-started/installation',
            label: 'Get Started',
            position: 'left',
          },
          {
            to: '/docs/reference/forecastpolicy',
            label: 'CRD Reference',
            position: 'left',
          },
          {
            to: '/docs/cli/reference',
            label: 'CLI',
            position: 'left',
          },
          {
            href: 'https://github.com/srujan-rai/scalepilot',
            label: 'GitHub',
            position: 'right',
            className: 'header-github-link',
            'aria-label': 'GitHub repository',
          },
        ],
      },

      footer: {
        style: 'dark',
        links: [
          {
            title: 'Documentation',
            items: [
              { label: 'Introduction', to: '/docs/intro' },
              { label: 'Installation', to: '/docs/getting-started/installation' },
              { label: 'Quick Start', to: '/docs/getting-started/quick-start' },
            ],
          },
          {
            title: 'Features',
            items: [
              { label: 'Predictive Scaling', to: '/docs/features/predictive-scaling' },
              { label: 'Multi-Cluster Federation', to: '/docs/features/multi-cluster-federation' },
              { label: 'FinOps Budgets', to: '/docs/features/finops-budgets' },
            ],
          },
          {
            title: 'Reference',
            items: [
              { label: 'ForecastPolicy CRD', to: '/docs/reference/forecastpolicy' },
              { label: 'CLI Reference', to: '/docs/cli/reference' },
              { label: 'ARIMA Algorithm', to: '/docs/algorithms/arima' },
              { label: 'Holt-Winters Algorithm', to: '/docs/algorithms/holt-winters' },
            ],
          },
          {
            title: 'Community',
            items: [
              {
                label: 'GitHub',
                href: 'https://github.com/srujan-rai/scalepilot',
              },
              {
                label: 'Issues',
                href: 'https://github.com/srujan-rai/scalepilot/issues',
              },
              {
                label: 'Contributing',
                to: '/docs/contributing/development',
              },
            ],
          },
        ],
        copyright: `Copyright © ${new Date().getFullYear()} ScalePilot Contributors. Built with Docusaurus. Licensed under Apache 2.0.`,
      },

      prism: {
        theme: prismThemes.github,
        darkTheme: prismThemes.dracula,
        additionalLanguages: ['bash', 'yaml', 'go', 'docker', 'makefile', 'json'],
      },

      mermaid: {
        theme: { light: 'neutral', dark: 'dark' },
      },

      algolia: undefined,

      tableOfContents: {
        minHeadingLevel: 2,
        maxHeadingLevel: 4,
      },
    }),
};

export default config;
