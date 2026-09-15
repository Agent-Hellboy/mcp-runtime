import type { UsageTotals } from "../../api/types";

type UsageSummaryProps = {
  totals: UsageTotals;
};

function denyRate(totals: UsageTotals): string {
  if (!totals.events) {
    return "0%";
  }
  return `${Math.round((totals.denied / totals.events) * 100)}%`;
}

export function UsageSummary({ totals }: UsageSummaryProps) {
  const metrics = [
    { label: "Requests", value: totals.events },
    { label: "Allowed", value: totals.allowed },
    { label: "Denied", value: totals.denied },
    { label: "Servers", value: totals.unique_servers },
    { label: "Deny rate", value: denyRate(totals) },
  ];

  return (
    <ul className="metric-row" aria-label="Activity summary" data-testid="usage-summary">
      {metrics.map((metric) => (
        <li key={metric.label}>
          <span className="metric-label">{metric.label}</span>
          <strong className="metric-value">{metric.value}</strong>
        </li>
      ))}
    </ul>
  );
}
