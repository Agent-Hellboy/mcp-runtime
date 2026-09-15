import { useId, useState } from "react";

import { AdminTable, type AdminColumn } from "./AdminTable";
import { AsyncSection } from "./AsyncSection";
import { StatusBadge } from "../StatusBadge";
import { useAdminReload, useGrants, useSessions } from "../../hooks/useAdminData";
import { accessKey, subjectLabel } from "../../api/types";
import type { GrantSummary, SessionSummary } from "../../api/types";

export type AccessSelection =
  | { kind: "grant"; item: GrantSummary }
  | { kind: "session"; item: SessionSummary };

type AccessControlPanelProps = {
  namespace: string;
  onNamespaceChange: (namespace: string) => void;
  onSelect: (selection: AccessSelection) => void;
  onSignIn: () => void;
};

export function AccessControlPanel({
  namespace,
  onNamespaceChange,
  onSelect,
  onSignIn,
}: AccessControlPanelProps) {
  const [filter, setFilter] = useState("");
  const filterId = useId();
  const namespaceId = useId();
  const reload = useAdminReload();

  const grantsQuery = useGrants(true, namespace);
  const sessionsQuery = useSessions(true, namespace);

  const grants = grantsQuery.data ?? [];
  const sessions = sessionsQuery.data ?? [];

  const term = filter.trim().toLowerCase();
  const matches = (parts: Array<string | undefined>) =>
    !term || parts.filter(Boolean).join(" ").toLowerCase().includes(term);

  const visibleGrants = grants.filter((grant) =>
    matches([grant.name, grant.namespace, grant.serverRef?.name, subjectLabel(grant.subject)])
  );
  const visibleSessions = sessions.filter((session) =>
    matches([
      session.name,
      session.namespace,
      session.serverRef?.name,
      subjectLabel(session.subject),
    ])
  );

  const activeGrants = grants.filter((grant) => !grant.disabled).length;
  const activeSessions = sessions.filter((session) => !session.revoked).length;

  const grantColumns: Array<AdminColumn<GrantSummary>> = [
    {
      id: "name",
      header: "Grant",
      rowHeader: true,
      cell: (grant) => (
        <button
          type="button"
          className="link-button"
          data-testid="grant-drilldown"
          onClick={() => onSelect({ kind: "grant", item: grant })}
        >
          {grant.name}
        </button>
      ),
    },
    { id: "server", header: "Server", cell: (grant) => grant.serverRef?.name || "—" },
    { id: "subject", header: "Subject", cell: (grant) => subjectLabel(grant.subject) },
    { id: "trust", header: "Max trust", cell: (grant) => grant.maxTrust || "—" },
    {
      id: "effects",
      header: "Side effects",
      cell: (grant) => (grant.allowedSideEffects || []).join(", ") || "—",
    },
    {
      id: "status",
      header: "Status",
      cell: (grant) => (
        <StatusBadge tone={grant.disabled ? "attention" : "ready"}>
          {grant.disabled ? "Disabled" : "Active"}
        </StatusBadge>
      ),
    },
  ];

  const sessionColumns: Array<AdminColumn<SessionSummary>> = [
    {
      id: "name",
      header: "Session",
      rowHeader: true,
      cell: (session) => (
        <button
          type="button"
          className="link-button"
          data-testid="session-drilldown"
          onClick={() => onSelect({ kind: "session", item: session })}
        >
          {session.name}
        </button>
      ),
    },
    { id: "server", header: "Server", cell: (session) => session.serverRef?.name || "—" },
    { id: "subject", header: "Subject", cell: (session) => subjectLabel(session.subject) },
    { id: "trust", header: "Trust", cell: (session) => session.consentedTrust || "—" },
    { id: "expires", header: "Expires", cell: (session) => session.expiresAt || "—" },
    {
      id: "status",
      header: "Status",
      cell: (session) => (
        <StatusBadge tone={session.revoked ? "attention" : "ready"}>
          {session.revoked ? "Revoked" : "Active"}
        </StatusBadge>
      ),
    },
  ];

  return (
    <section className="panel" aria-labelledby="access-control-title">
      <div className="panel-head">
        <div>
          <h2 id="access-control-title">Access control</h2>
          <p className="panel-lede">
            Grants and agent sessions enforced by the MCP gateway. Select one to trace the
            decisions it produced.
          </p>
        </div>
        <ul className="stat-row" aria-label="Access control summary" data-testid="access-stats">
          <li>
            <strong>{activeGrants}</strong> active grants
          </li>
          <li>
            <strong>{grants.length - activeGrants}</strong> disabled
          </li>
          <li>
            <strong>{activeSessions}</strong> active sessions
          </li>
          <li>
            <strong>{sessions.length - activeSessions}</strong> revoked
          </li>
        </ul>
      </div>

      <form className="toolbar" role="search" onSubmit={(event) => event.preventDefault()}>
        <div className="field grow">
          <label htmlFor={filterId}>Filter</label>
          <input
            id={filterId}
            type="search"
            placeholder="Name, server, or subject"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            data-testid="access-filter"
          />
        </div>
        <div className="field">
          <label htmlFor={namespaceId}>Namespace</label>
          <input
            id={namespaceId}
            type="search"
            placeholder="All namespaces"
            value={namespace}
            onChange={(event) => onNamespaceChange(event.target.value)}
            data-testid="access-namespace"
          />
        </div>
        <button type="button" className="button ghost" onClick={reload} data-testid="access-refresh">
          Refresh
        </button>
      </form>

      <h3 className="subsection-title">Access grants</h3>
      <AsyncSection
        query={grantsQuery}
        loadingLabel="Loading access grants…"
        errorTitle="Access grants could not be loaded."
        onRetry={reload}
        onSignIn={onSignIn}
        testId="grants"
      >
        <AdminTable
          caption="Access grants with server, subject, trust ceiling, allowed side effects, and status."
          columns={grantColumns}
          rows={visibleGrants}
          rowKey={accessKey}
          emptyMessage={
            grants.length === 0 ? "No access grants found." : "No grants match this filter."
          }
          testId="grants-table"
        />
      </AsyncSection>

      <h3 className="subsection-title">Agent sessions</h3>
      <AsyncSection
        query={sessionsQuery}
        loadingLabel="Loading agent sessions…"
        errorTitle="Agent sessions could not be loaded."
        onRetry={reload}
        onSignIn={onSignIn}
        testId="sessions"
      >
        <AdminTable
          caption="Agent sessions with server, subject, consented trust, expiry, and status."
          columns={sessionColumns}
          rows={visibleSessions}
          rowKey={accessKey}
          emptyMessage={
            sessions.length === 0 ? "No agent sessions found." : "No sessions match this filter."
          }
          testId="sessions-table"
        />
      </AsyncSection>

      <p className="panel-footnote" data-testid="access-mutation-note">
        Disabling a grant or revoking a session is still handled in the legacy dashboard under
        More workspaces. Those write paths need a CSRF-protected route before they move here.
      </p>
    </section>
  );
}
