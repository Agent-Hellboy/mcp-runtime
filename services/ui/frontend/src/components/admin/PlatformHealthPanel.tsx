import { useState } from "react";

import { AdminTable, type AdminColumn } from "./AdminTable";
import { AsyncSection } from "./AsyncSection";
import { StatusBadge } from "../StatusBadge";
import { useAdminReload, useComponents } from "../../hooks/useAdminData";
import { restartComponent } from "../../api/admin";
import type { ComponentStatus } from "../../api/types";

type PlatformHealthPanelProps = {
  onSignIn: () => void;
};

function componentReady(component: ComponentStatus): boolean {
  return (component.status || "").toLowerCase() === "ready";
}

const COLUMNS: Array<AdminColumn<ComponentStatus>> = [
  {
    id: "display",
    header: "Component",
    rowHeader: true,
    cell: (component) => component.display || component.key,
  },
  {
    id: "status",
    header: "Status",
    cell: (component) => (
      <StatusBadge tone={componentReady(component) ? "ready" : "attention"}>
        {component.status || "Unknown"}
      </StatusBadge>
    ),
  },
  { id: "ready", header: "Ready", cell: (component) => component.ready || "—" },
  {
    id: "resource",
    header: "Resource",
    cell: (component) => (
      <code className="cell-code">
        {component.namespace} / {component.resource}
      </code>
    ),
  },
  { id: "message", header: "Message", cell: (component) => component.message || "—" },
];

// Grafana stays a plain link to the platform ingress path so it keeps the
// existing forward-auth behaviour. The dashboard must not proxy it.
export function PlatformHealthPanel({ onSignIn }: PlatformHealthPanelProps) {
  const reload = useAdminReload();
  const componentsQuery = useComponents(true);
  const components = componentsQuery.data ?? [];
  const readyCount = components.filter(componentReady).length;
  const [busyKey, setBusyKey] = useState("");
  const [error, setError] = useState("");

  async function restart(key?: string): Promise<void> {
    const label = key ? components.find((item) => item.key === key)?.display || key : "all components";
    if (!window.confirm(`Restart ${label}?`)) return;
    setBusyKey(key || "all"); setError("");
    try { await restartComponent(key); reload(); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Component restart failed."); }
    finally { setBusyKey(""); }
  }

  return (
    <section className="panel" aria-labelledby="platform-title">
      <div className="panel-head">
        <div>
          <h2 id="platform-title">Platform health</h2>
          <p className="panel-lede">
            Operator, Sentinel services, and observability components.
          </p>
        </div>
        <ul className="stat-row" aria-label="Platform summary" data-testid="platform-stats">
          <li>
            <strong>{readyCount}</strong> ready
          </li>
          <li>
            <strong>{components.length - readyCount}</strong> degraded
          </li>
        </ul>
      </div>

      <nav className="admin-links" aria-label="Platform actions and observability tools">
        <button type="button" className="button danger" data-testid="restart-all" disabled={busyKey !== ""} onClick={() => void restart()}>{busyKey === "all" ? "Restarting…" : "Restart all"}</button>
        <a
          className="button ghost"
          href="/grafana"
          target="_blank"
          rel="noreferrer"
          data-testid="grafana-link"
        >
          Grafana
        </a>
        <a
          className="button ghost"
          href="/prometheus"
          target="_blank"
          rel="noreferrer"
          data-testid="prometheus-link"
        >
          Prometheus
        </a>
      </nav>

      {error ? <p className="inline-error" role="alert" data-testid="platform-action-error">{error}</p> : null}

      <AsyncSection
        query={componentsQuery}
        loadingLabel="Loading platform components…"
        errorTitle="Platform components could not be loaded."
        onRetry={reload}
        onSignIn={onSignIn}
        testId="platform"
      >
        <AdminTable
          caption="Sentinel platform components with readiness, backing resource, and status message."
          columns={COLUMNS}
          rows={components}
          rowKey={(component) => component.key}
          emptyMessage="No platform components reported."
          testId="platform-table"
        />
      </AsyncSection>

      <div className="admin-component-actions" aria-label="Restart individual components">
        {components.map((component) => (
          <button key={component.key} type="button" className="button ghost compact" data-testid={`restart-${component.key}`} disabled={busyKey !== ""} onClick={() => void restart(component.key)}>
            {busyKey === component.key ? "Restarting…" : `Restart ${component.display || component.key}`}
          </button>
        ))}
      </div>

      <p className="panel-footnote">
        Restarting a component rolls its workload and may briefly interrupt traffic.
      </p>
    </section>
  );
}
