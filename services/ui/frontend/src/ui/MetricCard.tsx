import type { ReactNode } from "react";

import { Icon, type IconName } from "./Icon";

export type MetricTone = "default" | "success" | "warning" | "danger";

export type Metric = {
  label: string;
  // `undefined` means the value is genuinely unknown (loading or unavailable).
  // It renders as "—", never as a healthy-looking zero.
  value: number | string | undefined;
  hint?: string;
  icon?: IconName;
  tone?: MetricTone;
  testId?: string;
};

export function MetricCard({ label, value, hint, icon, tone = "default", testId }: Metric) {
  const unknown = value === undefined || value === null || value === "";
  return (
    <div className={tone === "default" ? "metric-card" : `metric-card tone-${tone}`} data-testid={testId}>
      <span className="metric-label">
        {icon ? <Icon name={icon} size={14} /> : null}
        {label}
      </span>
      <strong className={unknown ? "metric-value is-unknown" : "metric-value"}>
        {unknown ? "—" : typeof value === "number" ? value.toLocaleString() : value}
      </strong>
      {hint ? <p className="metric-hint">{hint}</p> : null}
    </div>
  );
}

type MetricGridProps = {
  metrics: Metric[];
  label: string;
  testId?: string;
  children?: ReactNode;
};

export function MetricGrid({ metrics, label, testId, children }: MetricGridProps) {
  return (
    <div className="metric-grid" role="group" aria-label={label} data-testid={testId}>
      {metrics.map((metric) => (
        <MetricCard key={metric.label} {...metric} />
      ))}
      {children}
    </div>
  );
}
