import React from 'react';
import Link from '@docusaurus/Link';
import styles from './styles.module.css';

const features = [
  {
    icon: '📈',
    iconClass: 'blue',
    title: 'Predictive Scaling',
    description:
      'Train ARIMA or Holt-Winters models on your Prometheus history. ScalePilot pre-scales HPA minReplicas minutes before traffic spikes arrive, so your pods are warm and ready — not catching up.',
    link: '/docs/features/predictive-scaling',
    linkText: 'Learn about ForecastPolicy',
    bullets: [
      'ARIMA(p,d,q) with Yule-Walker estimation',
      'Holt-Winters triple exponential smoothing',
      '95% confidence intervals for conservative scaling',
      'SLO-aware ScaleUpGuard gate',
    ],
  },
  {
    icon: '🌐',
    iconClass: 'green',
    title: 'Multi-Cluster Federation',
    description:
      'When a queue depth or load metric exceeds your threshold, ScalePilot automatically spills workloads to overflow clusters — ordered by priority, capped by maxCapacity, with a cooldown to prevent thrashing.',
    link: '/docs/features/multi-cluster-federation',
    linkText: 'Learn about FederatedScaledObject',
    bullets: [
      'Metric-driven spillover (any PromQL query)',
      'Priority-ordered overflow cluster selection',
      'Automatic health-checking every 30s',
      'Server-side apply for idempotent cross-cluster writes',
    ],
  },
  {
    icon: '💰',
    iconClass: 'orange',
    title: 'FinOps ScalingBudget',
    description:
      'Set a monthly cost ceiling per namespace. ScalePilot polls AWS, GCP, or Azure billing APIs, computes real spend, and enforces Delay / Downgrade / Block actions automatically when you breach the limit.',
    link: '/docs/features/finops-budgets',
    linkText: 'Learn about ScalingBudget',
    bullets: [
      'AWS Cost Explorer, GCP Billing, Azure Cost Management',
      'Millidollar precision avoids float rounding',
      'Slack & PagerDuty breach notifications',
      'Warning threshold before hard ceiling',
    ],
  },
];

function FeatureCard({ icon, iconClass, title, description, link, linkText, bullets }) {
  return (
    <div className="feature-card">
      <div className={`feature-card-icon feature-card-icon--${iconClass}`}>
        {icon}
      </div>
      <div className="feature-card-title">{title}</div>
      <p className="feature-card-description">{description}</p>
      <ul className={styles.bulletList}>
        {bullets.map((b, i) => (
          <li key={i}>{b}</li>
        ))}
      </ul>
      <Link to={link} className="feature-card-link">
        {linkText} →
      </Link>
    </div>
  );
}

export default function HomepageFeatures() {
  return (
    <section className="features-section">
      <div className="container">
        <div className="section-header">
          <div className="section-header__eyebrow">Core Features</div>
          <h2 className="section-header__title">Three things Kubernetes HPA can't do</h2>
          <p className="section-header__subtitle">
            ScalePilot adds proactive intelligence, geographic reach, and cost discipline to your existing autoscaling stack.
          </p>
        </div>
        <div className="features-grid">
          {features.map((f, i) => (
            <FeatureCard key={i} {...f} />
          ))}
        </div>
      </div>
    </section>
  );
}
