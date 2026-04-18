import React from 'react';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import HomepageFeatures from '@site/src/components/HomepageFeatures';
import styles from './index.module.css';

const comparisonRows = [
  {
    capability: 'Reactive scaling (metric-based)',
    hpa: true, keda: true, kubecost: false, admiralty: false, scalepilot: true,
  },
  {
    capability: 'Predictive scaling (time-series forecast)',
    hpa: false, keda: false, kubecost: false, admiralty: false, scalepilot: true,
  },
  {
    capability: 'Multi-cluster workload spillover',
    hpa: false, keda: false, kubecost: false, admiralty: 'Placement-based', scalepilot: 'Metric-driven',
  },
  {
    capability: 'Cost observability',
    hpa: false, keda: false, kubecost: true, admiralty: false, scalepilot: true,
  },
  {
    capability: 'Cost enforcement (block/delay scaling)',
    hpa: false, keda: false, kubecost: false, admiralty: false, scalepilot: true,
  },
  {
    capability: 'Blackout windows (cron-based)',
    hpa: false, keda: false, kubecost: false, admiralty: false, scalepilot: true,
  },
  {
    capability: 'Per-team scaling governance',
    hpa: false, keda: false, kubecost: false, admiralty: false, scalepilot: true,
  },
];

function CellValue({ val }) {
  if (val === true) return <span className="check">✓</span>;
  if (val === false) return <span className="cross">—</span>;
  return <span style={{ fontSize: '0.85rem', color: '#64748b' }}>{val}</span>;
}

function ComparisonTable() {
  return (
    <section className="comparison-section">
      <div className="container">
        <div className="section-header">
          <div className="section-header__eyebrow">Comparison</div>
          <h2 className="section-header__title">How ScalePilot compares</h2>
          <p className="section-header__subtitle">
            ScalePilot is the only open-source operator that combines proactive forecasting, geographic federation, and FinOps enforcement in a single controller.
          </p>
        </div>
        <div className="comparison-table-wrapper">
          <table className="comparison-table">
            <thead>
              <tr>
                <th>Capability</th>
                <th>HPA</th>
                <th>KEDA</th>
                <th>Kubecost</th>
                <th>Admiralty</th>
                <th style={{ background: '#1d4ed8' }}>ScalePilot</th>
              </tr>
            </thead>
            <tbody>
              {comparisonRows.map((row, i) => (
                <tr key={i}>
                  <td style={{ fontWeight: 500 }}>{row.capability}</td>
                  <td><CellValue val={row.hpa} /></td>
                  <td><CellValue val={row.keda} /></td>
                  <td><CellValue val={row.kubecost} /></td>
                  <td><CellValue val={row.admiralty} /></td>
                  <td className="highlight-col"><CellValue val={row.scalepilot} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  );
}

function StatsSection() {
  return (
    <section className="stats-section">
      <div className="container">
        <div className="stats-grid">
          <div>
            <div className="stat-number">4</div>
            <div className="stat-label">Custom Resource Definitions</div>
          </div>
          <div>
            <div className="stat-number">2</div>
            <div className="stat-label">Forecasting Algorithms</div>
          </div>
          <div>
            <div className="stat-number">3</div>
            <div className="stat-label">Cloud Providers Supported</div>
          </div>
          <div>
            <div className="stat-number">3–10m</div>
            <div className="stat-label">Predictive Lead Time</div>
          </div>
          <div>
            <div className="stat-number">Go 1.22</div>
            <div className="stat-label">Minimum Go Version</div>
          </div>
        </div>
      </div>
    </section>
  );
}

function HowItWorksSection() {
  return (
    <section className="features-section-dark">
      <div className="container">
        <div className="section-header">
          <div className="section-header__eyebrow">How It Works</div>
          <h2 className="section-header__title">Pre-scale before the spike, not after</h2>
          <p className="section-header__subtitle">
            HPA sees a CPU spike and adds pods — but those pods take 2–5 minutes to become ready. ScalePilot predicts the spike and pre-warms pods before it hits.
          </p>
        </div>
        <div className={styles.howItWorksGrid}>
          <div className={styles.howItWorksStep}>
            <div className={styles.stepNumber}>01</div>
            <h3 className={styles.stepTitle}>Collect History</h3>
            <p className={styles.stepDesc}>
              ForecastPolicy queries Prometheus for up to 7 days of metric history (request rate, queue depth, CPU — any PromQL scalar).
            </p>
          </div>
          <div className={styles.howItWorksArrow}>→</div>
          <div className={styles.howItWorksStep}>
            <div className={styles.stepNumber}>02</div>
            <h3 className={styles.stepTitle}>Train Model</h3>
            <p className={styles.stepDesc}>
              A background goroutine trains ARIMA(p,d,q) via Yule-Walker estimation or Holt-Winters triple exponential smoothing. The model is cached in a ConfigMap.
            </p>
          </div>
          <div className={styles.howItWorksArrow}>→</div>
          <div className={styles.howItWorksStep}>
            <div className={styles.stepNumber}>03</div>
            <h3 className={styles.stepTitle}>Predict & Pre-Scale</h3>
            <p className={styles.stepDesc}>
              Every reconcile cycle loads the cached model, predicts the next leadTimeMinutes, and patches HPA minReplicas — pods start warming up before traffic arrives.
            </p>
          </div>
          <div className={styles.howItWorksArrow}>→</div>
          <div className={styles.howItWorksStep}>
            <div className={styles.stepNumber}>04</div>
            <h3 className={styles.stepTitle}>Govern & Enforce</h3>
            <p className={styles.stepDesc}>
              ClusterScaleProfile enforces blackout windows and surge limits. ScalingBudget checks cloud billing and blocks, delays, or downgrades scaling if costs exceed the ceiling.
            </p>
          </div>
        </div>
      </div>
    </section>
  );
}

function CtaSection() {
  return (
    <section className="cta-section">
      <div className="container">
        <h2 className="cta-section__title">Ready to stop reacting to spikes?</h2>
        <p className="cta-section__subtitle">
          Install ScalePilot in under 5 minutes with Helm and start forecasting your first workload.
        </p>
        <div className="hero-cta-group">
          <Link to="/docs/getting-started/quick-start" className="hero-cta-primary">
            Quick Start Guide →
          </Link>
          <Link to="https://github.com/srujan-rai/scalepilot" className="hero-cta-secondary">
            View on GitHub
          </Link>
        </div>
      </div>
    </section>
  );
}

function HeroSection() {
  return (
    <header className="hero--scalepilot">
      <div className="container" style={{ textAlign: 'center', position: 'relative', zIndex: 1 }}>
        <div className="hero-badge">
          <span className="hero-badge-dot" />
          Open Source · Apache 2.0 · v0.1.0-alpha
        </div>
        <h1 className="hero__title">
          Pre-scale before<br />the spike
        </h1>
        <p className="hero__subtitle">
          ScalePilot is a cloud-agnostic Kubernetes autoscaling operator. It trains ARIMA and Holt-Winters models on your Prometheus history and patches HPA <code>minReplicas</code> minutes before traffic arrives — while enforcing FinOps cost budgets and multi-cluster spillover.
        </p>
        <div className="hero-cta-group">
          <Link to="/docs/getting-started/installation" className="hero-cta-primary">
            Get Started →
          </Link>
          <Link to="/docs/intro" className="hero-cta-secondary">
            Read the Docs
          </Link>
          <Link to="https://github.com/srujan-rai/scalepilot" className="hero-cta-secondary">
            GitHub
          </Link>
        </div>
        <div className={styles.heroInstallHint}>
          <code>helm install scalepilot charts/scalepilot --namespace scalepilot-system --create-namespace</code>
        </div>
      </div>
    </header>
  );
}

export default function Home() {
  const { siteConfig } = useDocusaurusContext();
  return (
    <Layout
      title={`${siteConfig.title} — Predictive Kubernetes Autoscaling`}
      description="Cloud-agnostic Kubernetes operator with ARIMA/Holt-Winters predictive scaling, multi-cluster federation, and FinOps cost budgets."
    >
      <HeroSection />
      <StatsSection />
      <HomepageFeatures />
      <HowItWorksSection />
      <ComparisonTable />
      <CtaSection />
    </Layout>
  );
}
