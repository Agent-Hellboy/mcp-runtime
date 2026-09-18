import { MetricGrid } from "../../ui/MetricCard";
import { percent } from "../../lib/format";
import type { UsageTotals } from "../../api/types";

type UsageMetricsProps = {
  totals: UsageTotals;
  /** Plain-language description of what these totals cover. */
  scope: string;
  testId?: string;
};

// Shared between tenant Activity and admin Analytics. The two screens differ in
// scope and authorization, not in how a total is presented.
export function UsageMetrics({ totals, scope, testId = "usage-summary" }: UsageMetricsProps) {
  const denyRate = totals.events ? `${Math.round((totals.denied / totals.events) * 100)}%` : "0%";

  return (
    <MetricGrid
      label={`Usage summary for ${scope}`}
      testId={testId}
      metrics={[
        { label: "Requests", value: totals.events, icon: "activity", hint: scope },
        { label: "Allowed", value: totals.allowed, icon: "check", tone: "success" },
        {
          label: "Denied",
          value: totals.denied,
          icon: "shield",
          tone: totals.denied > 0 ? "warning" : "default",
        },
        { label: "Servers", value: totals.unique_servers, icon: "server" },
        {
          label: "Deny rate",
          value: denyRate,
          icon: "gauge",
          hint: totals.events ? undefined : "No requests in this window",
        },
      ]}
    />
  );
}
