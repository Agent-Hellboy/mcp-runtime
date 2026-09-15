import { useId, useState } from "react";

import { AdminTable, type AdminColumn } from "./AdminTable";
import { AsyncSection } from "./AsyncSection";
import { StatusBadge } from "../StatusBadge";
import { useAdminReload, useOperations } from "../../hooks/useAdminData";
import type { AuditLogEntry, ImageActivity, UserActivity } from "../../api/types";

type OperationsPanelProps = {
  onSignIn: () => void;
};

function formatTime(value: string | undefined): string {
  if (!value) {
    return "—";
  }
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

function statusTone(status: string): "ready" | "attention" | "neutral" {
  const normalized = (status || "").toLowerCase();
  if (normalized === "success" || normalized === "ok") {
    return "ready";
  }
  if (normalized === "error" || normalized === "failed" || normalized === "denied") {
    return "attention";
  }
  return "neutral";
}

const USER_COLUMNS: Array<AdminColumn<UserActivity>> = [
  { id: "email", header: "User", rowHeader: true, cell: (user) => user.email },
  { id: "role", header: "Role", cell: (user) => user.role || "—" },
  { id: "namespace", header: "Namespace", cell: (user) => user.namespace || "—" },
  { id: "logins", header: "Logins", cell: (user) => String(user.login_count ?? 0) },
  { id: "failures", header: "Failed actions", cell: (user) => String(user.failed_action_count ?? 0) },
  { id: "keys", header: "API keys", cell: (user) => String(user.api_keys ?? 0) },
  { id: "last", header: "Last activity", cell: (user) => formatTime(user.last_activity_at) },
];

const AUDIT_COLUMNS: Array<AdminColumn<AuditLogEntry>> = [
  { id: "time", header: "Time", rowHeader: true, cell: (entry) => formatTime(entry.created_at) },
  { id: "action", header: "Action", cell: (entry) => entry.action || "—" },
  { id: "resource", header: "Resource", cell: (entry) => entry.resource || "—" },
  { id: "namespace", header: "Namespace", cell: (entry) => entry.namespace || "—" },
  { id: "identity", header: "Identity", cell: (entry) => entry.auth_identity || entry.user_id || "—" },
  {
    id: "status",
    header: "Status",
    cell: (entry) => (
      <StatusBadge tone={statusTone(entry.status)}>{entry.status || "—"}</StatusBadge>
    ),
  },
];

const IMAGE_COLUMNS: Array<AdminColumn<ImageActivity>> = [
  {
    id: "image",
    header: "Image",
    rowHeader: true,
    cell: (image) => <code className="cell-code">{image.image_ref}</code>,
  },
  { id: "server", header: "Server", cell: (image) => image.server_name || "—" },
  { id: "namespace", header: "Namespace", cell: (image) => image.namespace || "—" },
  { id: "email", header: "By", cell: (image) => image.email || "—" },
  { id: "action", header: "Action", cell: (image) => image.action || "—" },
  {
    id: "status",
    header: "Status",
    cell: (image) => (
      <StatusBadge tone={statusTone(image.status)}>{image.status || "—"}</StatusBadge>
    ),
  },
  { id: "time", header: "When", cell: (image) => formatTime(image.created_at) },
];

export function OperationsPanel({ onSignIn }: OperationsPanelProps) {
  const [userFilter, setUserFilter] = useState("");
  const [appliedUser, setAppliedUser] = useState("");
  const userId = useId();
  const reload = useAdminReload();

  const operationsQuery = useOperations(true, appliedUser);
  const operations = operationsQuery.data;

  return (
    <section className="panel" aria-labelledby="operations-title">
      <div className="panel-head">
        <div>
          <h2 id="operations-title">Operations</h2>
          <p className="panel-lede">
            Platform users, the audit trail, and image publish activity.
          </p>
        </div>
        <ul className="stat-row" aria-label="Operations summary" data-testid="operations-stats">
          <li>
            <strong>{operations?.users.length ?? 0}</strong> users
          </li>
          <li>
            <strong>{operations?.audit_logs.length ?? 0}</strong> audit entries
          </li>
          <li>
            <strong>{operations?.images.length ?? 0}</strong> image events
          </li>
        </ul>
      </div>

      <form
        className="toolbar"
        role="search"
        onSubmit={(event) => {
          event.preventDefault();
          setAppliedUser(userFilter.trim());
        }}
      >
        <div className="field grow">
          <label htmlFor={userId}>Filter by user</label>
          <input
            id={userId}
            type="search"
            placeholder="Email address"
            value={userFilter}
            onChange={(event) => setUserFilter(event.target.value)}
            data-testid="operations-user-filter"
          />
        </div>
        <button type="submit" className="button primary" data-testid="operations-apply">
          Apply
        </button>
        <button
          type="button"
          className="button ghost"
          onClick={reload}
          data-testid="operations-refresh"
        >
          Refresh
        </button>
      </form>

      <AsyncSection
        query={operationsQuery}
        loadingLabel="Loading platform operations…"
        errorTitle="Platform operations could not be loaded."
        onRetry={reload}
        onSignIn={onSignIn}
        testId="operations"
      >
        <>
          <h3 className="subsection-title">Users</h3>
          <AdminTable
            caption="Platform users with role, namespace, login counts, and last activity."
            columns={USER_COLUMNS}
            rows={operations?.users ?? []}
            rowKey={(user) => user.id || user.email}
            emptyMessage="No users found."
            testId="operations-users-table"
          />

          <h3 className="subsection-title">Audit trail</h3>
          <AdminTable
            caption="Recent platform audit entries with action, resource, identity, and status."
            columns={AUDIT_COLUMNS}
            rows={operations?.audit_logs ?? []}
            rowKey={(entry) =>
              `${entry.created_at || ""}-${entry.action}-${entry.resource}-${entry.user_id || ""}`
            }
            emptyMessage="No audit entries found."
            testId="operations-audit-table"
          />

          <h3 className="subsection-title">Image activity</h3>
          <AdminTable
            caption="Image publish and deployment activity with the user and resulting status."
            columns={IMAGE_COLUMNS}
            rows={operations?.images ?? []}
            rowKey={(image) => `${image.created_at || ""}-${image.image_ref}-${image.action}`}
            emptyMessage="No image activity found."
            testId="operations-images-table"
          />
        </>
      </AsyncSection>
    </section>
  );
}
