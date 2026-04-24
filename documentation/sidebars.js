// @ts-check

/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  docsSidebar: [
    {
      type: 'doc',
      id: 'intro',
      label: 'Introduction',
    },
    {
      type: 'category',
      label: 'Getting Started',
      collapsed: false,
      items: [
        'getting-started/prerequisites',
        'getting-started/installation',
        'getting-started/quick-start',
      ],
    },
    {
      type: 'category',
      label: 'Features',
      collapsed: false,
      items: [
        'features/predictive-scaling',
        'features/multi-cluster-federation',
        'features/finops-budgets',
      ],
    },
    {
      type: 'category',
      label: 'CRD Reference',
      collapsed: false,
      items: [
        'reference/forecastpolicy',
        'reference/federatedscaledobject',
        'reference/scalingbudget',
        'reference/clusterscaleprofile',
      ],
    },
    {
      type: 'category',
      label: 'CLI Reference',
      items: [
        'cli/reference',
      ],
    },
    {
      type: 'category',
      label: 'Algorithms',
      items: [
        'algorithms/arima',
        'algorithms/holt-winters',
      ],
    },
    {
      type: 'category',
      label: 'Deployment',
      items: [
        'deployment/helm',
        'deployment/github-actions',
      ],
    },
    {
      type: 'category',
      label: 'Contributing',
      items: [
        'contributing/development',
      ],
    },
    {
      type: 'doc',
      id: 'spike-test-report',
      label: 'Spike Test Report',
    },
  ],
};

export default sidebars;
