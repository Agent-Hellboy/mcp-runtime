import { AdminTable, type AdminColumn } from "./AdminTable";
import { AsyncSection } from "./AsyncSection";
import { StatusBadge } from "../StatusBadge";
import { useAdminReload, useComponents } from "../../hooks/useAdminData";
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

      <nav className="admin-links" aria-label="Observability tools">
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

      <p className="panel-footnote">
        Restarting a component is a destructive operation and stays in the legacy dashboard
        under More workspaces until a CSRF-protected route exists.
      </p>
    </section>
  );
}
