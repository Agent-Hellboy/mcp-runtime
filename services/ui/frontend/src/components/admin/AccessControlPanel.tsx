import { useEffect, useId, useState } from "react";

import { AdminTable, type AdminColumn } from "./AdminTable";
import { AsyncSection } from "./AsyncSection";
import { StatusBadge } from "../StatusBadge";
import { useAdminReload, useGrants, useSessions } from "../../hooks/useAdminData";
import { createGrant, createSession, createTeam, createTeamUser, listTeamMembers, listTeams, setGrantDisabled, setSessionRevoked } from "../../api/admin";
import { accessKey, subjectLabel } from "../../api/types";
import type { GrantSummary, SessionSummary, TeamMembership, TeamRecord } from "../../api/types";

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
  const [teams, setTeams] = useState<TeamRecord[]>([]);
  const [members, setMembers] = useState<TeamMembership[]>([]);
  const [grantSubjectType, setGrantSubjectType] = useState<"user" | "team">("user");
  const [sessionSubjectType, setSessionSubjectType] = useState<"user" | "team">("user");
  const [identityForm, setIdentityForm] = useState<"team" | "user" | "">("");
  const filterId = useId();
  const namespaceId = useId();
  const reload = useAdminReload();

  const grantsQuery = useGrants(true, namespace);
  const sessionsQuery = useSessions(true, namespace);

  const grants = grantsQuery.data ?? [];
  const sessions = sessionsQuery.data ?? [];

  useEffect(() => {
    let active = true;
    void listTeams()
      .then(async (records) => {
        setTeams(records);
        const memberLists = await Promise.allSettled(records.map((team) => listTeamMembers(team.slug)));
        if (active) {
          setMembers(memberLists.flatMap((result) => result.status === "fulfilled" ? result.value : []));
        }
      })
      .catch(() => {
        // The access tables remain useful if the optional identity lookup is unavailable.
      });
    return () => {
      active = false;
    };
  }, []);

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
    const subjectID = String(form.get("subjectID") || "").trim();
    if (!name || !server || !subjectID) { setActionError("Grant name, server, and a user or team subject are required."); return; }
    setBusyKey("create-grant"); setActionError("");
    try {
      await createGrant({ name, namespace: String(form.get("namespace") || namespace || "mcp-servers").trim(), serverRef: { name: server }, subject: grantSubjectType === "team" ? { teamID: subjectID } : { humanID: subjectID }, maxTrust: String(form.get("maxTrust") || "low"), allowedSideEffects: ["read"] });
      setShowGrantForm(false); event.currentTarget.reset(); reload();
    } catch (error) { setActionError(error instanceof Error ? error.message : "Grant creation failed."); }
    finally { setBusyKey(""); }
  }

  async function applySession(event: React.FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const name = String(form.get("name") || "").trim();
    const server = String(form.get("server") || "").trim();
    const subjectID = String(form.get("subjectID") || "").trim();
    if (!name || !server || !subjectID) { setActionError("Session name, server, and a user or team subject are required."); return; }
    const rawExpiry = String(form.get("expiresAt") || "");
    setBusyKey("create-session"); setActionError("");
    try {
      await createSession({ name, namespace: String(form.get("namespace") || namespace || "mcp-servers").trim(), serverRef: { name: server }, subject: sessionSubjectType === "team" ? { teamID: subjectID } : { humanID: subjectID }, consentedTrust: String(form.get("trust") || "low"), expiresAt: rawExpiry ? new Date(rawExpiry).toISOString() : undefined });
      setShowSessionForm(false); event.currentTarget.reset(); reload();
    } catch (error) { setActionError(error instanceof Error ? error.message : "Session creation failed."); }
    finally { setBusyKey(""); }
  }

  function subjectFields(type: "user" | "team", setType: (value: "user" | "team") => void) {
    return <>
      <div className="field">
        <label htmlFor={`${type}-subject-type`}>Subject type</label>
        <select id={`${type}-subject-type`} name="subjectType" value={type} onChange={(event) => setType(event.target.value as "user" | "team")}>
          <option value="user">User</option>
          <option value="team">Team</option>
        </select>
      </div>
      <div className="field grow">
        <label htmlFor={`${type}-subject-id`}>{type === "team" ? "Team" : "User"}</label>
        <select id={`${type}-subject-id`} name="subjectID" required defaultValue="">
          <option value="">Select {type === "team" ? "a team" : "a user"}</option>
          {type === "team"
            ? teams.map((team) => <option key={team.id || team.slug} value={team.id || team.slug}>{team.name || team.slug} · {team.id || team.slug}</option>)
            : members.map((member) => <option key={member.user_id} value={member.user_id}>{member.email || member.user_id} · {member.user_id}</option>)}
        </select>
      </div>
    </>;
  }

  async function createIdentity(event: React.FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setBusyKey("create-identity");
    setActionError("");
    try {
      if (identityForm === "team") {
        await createTeam(String(form.get("identitySlug") || "").trim(), String(form.get("identityName") || "").trim());
      } else if (identityForm === "user") {
        const team = String(form.get("identityTeam") || "").trim();
        await createTeamUser(team, String(form.get("identityEmail") || "").trim(), String(form.get("identityPassword") || ""), String(form.get("identityRole") || "member"));
      }
      setIdentityForm("");
      const records = await listTeams();
      setTeams(records);
      const memberLists = await Promise.allSettled(records.map((team) => listTeamMembers(team.slug)));
      setMembers(memberLists.flatMap((result) => result.status === "fulfilled" ? result.value : []));
    } catch (error) {
      setActionError(error instanceof Error ? error.message : "Identity creation failed.");
    } finally {
      setBusyKey("");
    }
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
        <button type="button" className="button ghost" data-testid="identity-create-toggle" onClick={() => setIdentityForm((value) => value ? "" : "team")}>Create team or user</button>
      </div>
      {identityForm ? <form className="toolbar admin-form" onSubmit={(event) => void createIdentity(event)} data-testid="identity-create-form">
        <div className="field"><label htmlFor="identity-kind">Create</label><select id="identity-kind" value={identityForm} onChange={(event) => setIdentityForm(event.target.value as "team" | "user")}><option value="team">Team</option><option value="user">User in team</option></select></div>
        {identityForm === "team" ? <><div className="field"><label htmlFor="identity-slug">Team slug</label><input id="identity-slug" name="identitySlug" required /></div><div className="field grow"><label htmlFor="identity-name">Team name</label><input id="identity-name" name="identityName" required /></div></> : <><div className="field grow"><label htmlFor="identity-team">Team</label><select id="identity-team" name="identityTeam" required defaultValue=""><option value="">Select a team</option>{teams.map((team) => <option key={team.slug} value={team.slug}>{team.name || team.slug}</option>)}</select></div><div className="field grow"><label htmlFor="identity-email">Email</label><input id="identity-email" name="identityEmail" type="email" required /></div><div className="field"><label htmlFor="identity-password">Temporary password</label><input id="identity-password" name="identityPassword" type="password" minLength={8} required /></div><div className="field"><label htmlFor="identity-role">Role</label><select id="identity-role" name="identityRole" defaultValue="member"><option value="member">Member</option><option value="owner">Owner</option></select></div></>}
        <button className="button" disabled={busyKey === "create-identity"}>{busyKey === "create-identity" ? "Creating…" : "Create"}</button>
      </form> : null}
      {showGrantForm ? <form className="toolbar admin-form" onSubmit={(event) => void applyGrant(event)} data-testid="grant-create-form"><div className="field"><label htmlFor="grant-name">Name</label><input id="grant-name" name="name" required /></div><div className="field"><label htmlFor="grant-server">Server</label><input id="grant-server" name="server" required /></div>{subjectFields(grantSubjectType, setGrantSubjectType)}<div className="field"><label htmlFor="grant-trust">Max trust</label><select id="grant-trust" name="maxTrust" defaultValue="low"><option value="low">Low</option><option value="medium">Medium</option><option value="high">High</option></select></div><button className="button" disabled={busyKey === "create-grant"}>{busyKey === "create-grant" ? "Creating…" : "Create"}</button></form> : null}
      {showSessionForm ? <form className="toolbar admin-form" onSubmit={(event) => void applySession(event)} data-testid="session-create-form"><div className="field"><label htmlFor="session-name">Name</label><input id="session-name" name="name" required /></div><div className="field"><label htmlFor="session-server">Server</label><input id="session-server" name="server" required /></div>{subjectFields(sessionSubjectType, setSessionSubjectType)}<div className="field"><label htmlFor="session-trust">Trust</label><select id="session-trust" name="trust" defaultValue="low"><option value="low">Low</option><option value="medium">Medium</option><option value="high">High</option></select></div><div className="field"><label htmlFor="session-expires">Expires</label><input id="session-expires" name="expiresAt" type="datetime-local" /></div><button className="button" disabled={busyKey === "create-session"}>{busyKey === "create-session" ? "Creating…" : "Create"}</button></form> : null}

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
