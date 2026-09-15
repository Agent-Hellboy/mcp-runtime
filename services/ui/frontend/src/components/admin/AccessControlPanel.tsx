import { useId, useState } from "react";

import { AdminTable, type AdminColumn } from "./AdminTable";
import { AsyncSection } from "./AsyncSection";
import { StatusBadge } from "../StatusBadge";
import { useAdminReload, useGrants, useSessions } from "../../hooks/useAdminData";
import { createGrant, createSession, setGrantDisabled, setSessionRevoked } from "../../api/admin";
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
  const [busyKey, setBusyKey] = useState("");
  const [actionError, setActionError] = useState("");
  const [showGrantForm, setShowGrantForm] = useState(false);
  const [showSessionForm, setShowSessionForm] = useState(false);
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

  async function toggleGrant(grant: GrantSummary): Promise<void> {
    const disabled = !grant.disabled;
    if (!window.confirm(`${disabled ? "Disable" : "Enable"} grant \"${grant.name}\"?`)) return;
    const key = `grant:${accessKey(grant)}`;
    setBusyKey(key);
    setActionError("");
    try {
      await setGrantDisabled(grant.namespace, grant.name, disabled);
      reload();
    } catch (error) {
      setActionError(error instanceof Error ? error.message : "Grant update failed.");
    } finally {
      setBusyKey("");
    }
  }

  async function toggleSession(session: SessionSummary): Promise<void> {
    const revoked = !session.revoked;
    if (!window.confirm(`${revoked ? "Revoke" : "Unrevoke"} session \"${session.name}\"?`)) return;
    const key = `session:${accessKey(session)}`;
    setBusyKey(key);
    setActionError("");
    try {
      await setSessionRevoked(session.namespace, session.name, revoked);
      reload();
    } catch (error) {
      setActionError(error instanceof Error ? error.message : "Session update failed.");
    } finally {
      setBusyKey("");
    }
  }

  async function applyGrant(event: React.FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const name = String(form.get("name") || "").trim();
    const server = String(form.get("server") || "").trim();
    const humanID = String(form.get("humanID") || "").trim();
    const teamID = String(form.get("teamID") || "").trim();
    if (!name || !server || (!humanID && !teamID)) { setActionError("Grant name, server, and a human or team subject are required."); return; }
    setBusyKey("create-grant"); setActionError("");
    try {
      await createGrant({ name, namespace: String(form.get("namespace") || namespace || "mcp-servers").trim(), serverRef: { name: server }, subject: { humanID, teamID }, maxTrust: String(form.get("maxTrust") || "low"), allowedSideEffects: ["read"] });
      setShowGrantForm(false); event.currentTarget.reset(); reload();
    } catch (error) { setActionError(error instanceof Error ? error.message : "Grant creation failed."); }
    finally { setBusyKey(""); }
  }

  async function applySession(event: React.FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const name = String(form.get("name") || "").trim();
    const server = String(form.get("server") || "").trim();
    const humanID = String(form.get("humanID") || "").trim();
    const teamID = String(form.get("teamID") || "").trim();
    if (!name || !server || (!humanID && !teamID)) { setActionError("Session name, server, and a human or team subject are required."); return; }
    const rawExpiry = String(form.get("expiresAt") || "");
    setBusyKey("create-session"); setActionError("");
    try {
      await createSession({ name, namespace: String(form.get("namespace") || namespace || "mcp-servers").trim(), serverRef: { name: server }, subject: { humanID, teamID }, consentedTrust: String(form.get("trust") || "low"), expiresAt: rawExpiry ? new Date(rawExpiry).toISOString() : undefined });
      setShowSessionForm(false); event.currentTarget.reset(); reload();
    } catch (error) { setActionError(error instanceof Error ? error.message : "Session creation failed."); }
    finally { setBusyKey(""); }
  }

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
        <div className="admin-action-cell">
          <StatusBadge tone={grant.disabled ? "attention" : "ready"}>
            {grant.disabled ? "Disabled" : "Active"}
          </StatusBadge>
          <button
            type="button"
            className="button ghost compact"
            data-testid="grant-toggle"
            disabled={busyKey === `grant:${accessKey(grant)}`}
            onClick={() => void toggleGrant(grant)}
          >
            {busyKey === `grant:${accessKey(grant)}` ? "Saving…" : grant.disabled ? "Enable" : "Disable"}
          </button>
        </div>
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
        <div className="admin-action-cell">
          <StatusBadge tone={session.revoked ? "attention" : "ready"}>
            {session.revoked ? "Revoked" : "Active"}
          </StatusBadge>
          <button
            type="button"
            className="button ghost compact"
            data-testid="session-toggle"
            disabled={busyKey === `session:${accessKey(session)}`}
            onClick={() => void toggleSession(session)}
          >
            {busyKey === `session:${accessKey(session)}` ? "Saving…" : session.revoked ? "Unrevoke" : "Revoke"}
          </button>
        </div>
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

      <div className="admin-links" aria-label="Access control actions">
        <button type="button" className="button" data-testid="grant-create-toggle" onClick={() => setShowGrantForm((value) => !value)}>Create grant</button>
        <button type="button" className="button ghost" data-testid="session-create-toggle" onClick={() => setShowSessionForm((value) => !value)}>Create session</button>
      </div>
      {showGrantForm ? <form className="toolbar admin-form" onSubmit={(event) => void applyGrant(event)} data-testid="grant-create-form"><div className="field"><label htmlFor="grant-name">Name</label><input id="grant-name" name="name" required /></div><div className="field"><label htmlFor="grant-server">Server</label><input id="grant-server" name="server" required /></div><div className="field"><label htmlFor="grant-human">Human ID</label><input id="grant-human" name="humanID" /></div><div className="field"><label htmlFor="grant-team">Team ID</label><input id="grant-team" name="teamID" /></div><div className="field"><label htmlFor="grant-trust">Max trust</label><select id="grant-trust" name="maxTrust" defaultValue="low"><option value="low">Low</option><option value="medium">Medium</option><option value="high">High</option></select></div><button className="button" disabled={busyKey === "create-grant"}>{busyKey === "create-grant" ? "Creating…" : "Create"}</button></form> : null}
      {showSessionForm ? <form className="toolbar admin-form" onSubmit={(event) => void applySession(event)} data-testid="session-create-form"><div className="field"><label htmlFor="session-name">Name</label><input id="session-name" name="name" required /></div><div className="field"><label htmlFor="session-server">Server</label><input id="session-server" name="server" required /></div><div className="field"><label htmlFor="session-human">Human ID</label><input id="session-human" name="humanID" /></div><div className="field"><label htmlFor="session-team">Team ID</label><input id="session-team" name="teamID" /></div><div className="field"><label htmlFor="session-trust">Trust</label><select id="session-trust" name="trust" defaultValue="low"><option value="low">Low</option><option value="medium">Medium</option><option value="high">High</option></select></div><div className="field"><label htmlFor="session-expires">Expires</label><input id="session-expires" name="expiresAt" type="datetime-local" /></div><button className="button" disabled={busyKey === "create-session"}>{busyKey === "create-session" ? "Creating…" : "Create"}</button></form> : null}

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
      {actionError ? <p className="inline-error" role="alert" data-testid="access-action-error">{actionError}</p> : null}
    </section>
  );
}
