import { useEffect, useState } from "react";

import {
  createTeam,
  createTeamUser,
  listTeamMembers,
  removeTeamMember,
  setTeamMemberRole,
} from "../../api/admin";
import type { TeamMembership, TeamRecord } from "../../api/types";
import { useAdminReload, useTeams } from "../../hooks/useAdminData";
import { AdminTable, type AdminColumn } from "./AdminTable";
import { AsyncSection } from "./AsyncSection";

type TeamsPanelProps = { onSignIn: () => void };

const COLUMNS: Array<AdminColumn<TeamRecord>> = [
  { id: "name", header: "Team", rowHeader: true, cell: (team) => team.name || team.slug },
  { id: "slug", header: "Slug", cell: (team) => team.slug },
  { id: "namespace", header: "Namespace", cell: (team) => team.namespace || "—" },
  { id: "id", header: "ID", cell: (team) => <code className="cell-code">{team.id}</code> },
];

export function TeamsPanel({ onSignIn }: TeamsPanelProps) {
  const reload = useAdminReload();
  const teamsQuery = useTeams(true);
  const teams = teamsQuery.data ?? [];
  const [selectedSlug, setSelectedSlug] = useState("");
  const [members, setMembers] = useState<TeamMembership[]>([]);
  const [membersLoading, setMembersLoading] = useState(false);
  const [busyKey, setBusyKey] = useState("");
  const [error, setError] = useState("");
  const [teamFormOpen, setTeamFormOpen] = useState(false);
  const [userFormOpen, setUserFormOpen] = useState(false);

  useEffect(() => {
    if (!selectedSlug && teams[0]?.slug) setSelectedSlug(teams[0].slug);
    if (selectedSlug && !teams.some((team) => team.slug === selectedSlug)) setSelectedSlug(teams[0]?.slug || "");
  }, [selectedSlug, teams]);

  useEffect(() => {
    let active = true;
    if (!selectedSlug) { setMembers([]); return () => { active = false; }; }
    setMembersLoading(true);
    void listTeamMembers(selectedSlug)
      .then((value) => { if (active) setMembers(value); })
      .catch((cause: unknown) => { if (active) setError(cause instanceof Error ? cause.message : "Team members could not be loaded."); })
      .finally(() => { if (active) setMembersLoading(false); });
    return () => { active = false; };
  }, [selectedSlug]);

  async function refreshMembers(): Promise<void> {
    if (selectedSlug) setMembers(await listTeamMembers(selectedSlug));
  }

  async function submitTeam(event: React.FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const slug = String(form.get("slug") || "").trim();
    const name = String(form.get("name") || "").trim();
    if (!slug || !name) { setError("Team slug and name are required."); return; }
    setBusyKey("create-team"); setError("");
    try { await createTeam(slug, name); setTeamFormOpen(false); setSelectedSlug(slug); reload(); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Team creation failed."); }
    finally { setBusyKey(""); }
  }

  async function submitUser(event: React.FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const email = String(form.get("email") || "").trim();
    const password = String(form.get("password") || "");
    const role = String(form.get("role") || "member");
    if (!selectedSlug || !email || !password) { setError("Select a team and provide email and password."); return; }
    setBusyKey("create-user"); setError("");
    try { await createTeamUser(selectedSlug, email, password, role); event.currentTarget.reset(); await refreshMembers(); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Team user creation failed."); }
    finally { setBusyKey(""); }
  }

  async function changeRole(member: TeamMembership): Promise<void> {
    const role = member.role === "owner" ? "member" : "owner";
    if (!window.confirm(`Set ${member.email || member.user_id} to ${role}?`)) return;
    setBusyKey(`role:${member.user_id}`); setError("");
    try { await setTeamMemberRole(selectedSlug, member.user_id, role); await refreshMembers(); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Membership update failed."); }
    finally { setBusyKey(""); }
  }

  async function removeMember(member: TeamMembership): Promise<void> {
    if (!window.confirm(`Remove ${member.email || member.user_id} from ${selectedSlug}?`)) return;
    setBusyKey(`remove:${member.user_id}`); setError("");
    try { await removeTeamMember(selectedSlug, member.user_id); await refreshMembers(); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Membership removal failed."); }
    finally { setBusyKey(""); }
  }

  const memberColumns: Array<AdminColumn<TeamMembership>> = [
    { id: "email", header: "Member", rowHeader: true, cell: (member) => member.email || member.user_id },
    { id: "role", header: "Role", cell: (member) => member.role },
    { id: "id", header: "User ID", cell: (member) => <code className="cell-code">{member.user_id}</code> },
    { id: "actions", header: "Actions", cell: (member) => <div className="admin-action-cell"><button type="button" className="button ghost compact" disabled={busyKey !== ""} onClick={() => void changeRole(member)}>{member.role === "owner" ? "Set member" : "Make owner"}</button><button type="button" className="button ghost compact danger" disabled={busyKey !== ""} onClick={() => void removeMember(member)}>Remove</button></div> },
  ];

  return (
    <section className="panel" aria-labelledby="teams-title">
      <div className="panel-head"><div><h2 id="teams-title">Teams</h2><p className="panel-lede">Tenant teams, namespaces, and membership administration.</p></div><ul className="stat-row" aria-label="Team summary" data-testid="teams-stats"><li><strong>{teams.length}</strong> teams</li><li><strong>{members.length}</strong> selected members</li></ul></div>
      <div className="admin-links"><button type="button" className="button" data-testid="team-create-toggle" onClick={() => setTeamFormOpen((value) => !value)}>Create team</button><button type="button" className="button ghost" data-testid="team-user-toggle" disabled={!selectedSlug} onClick={() => setUserFormOpen((value) => !value)}>Add member</button></div>
      {teamFormOpen ? <form className="toolbar admin-form" onSubmit={(event) => void submitTeam(event)} data-testid="team-create-form"><div className="field"><label htmlFor="team-slug">Slug</label><input id="team-slug" name="slug" required /></div><div className="field grow"><label htmlFor="team-name">Name</label><input id="team-name" name="name" required /></div><button className="button" disabled={busyKey === "create-team"}>{busyKey === "create-team" ? "Creating…" : "Create"}</button></form> : null}
      {userFormOpen ? <form className="toolbar admin-form" onSubmit={(event) => void submitUser(event)} data-testid="team-user-form"><div className="field grow"><label htmlFor="team-user-email">Email</label><input id="team-user-email" name="email" type="email" required /></div><div className="field"><label htmlFor="team-user-password">Temporary password</label><input id="team-user-password" name="password" type="password" minLength={8} required /></div><div className="field"><label htmlFor="team-user-role">Role</label><select id="team-user-role" name="role" defaultValue="member"><option value="member">Member</option><option value="owner">Owner</option></select></div><button className="button" disabled={busyKey === "create-user"}>{busyKey === "create-user" ? "Adding…" : "Add"}</button></form> : null}
      {error ? <p className="inline-error" role="alert" data-testid="teams-action-error">{error}</p> : null}
      <AsyncSection query={teamsQuery} loadingLabel="Loading teams…" errorTitle="Teams could not be loaded." onRetry={reload} onSignIn={onSignIn} testId="teams"><AdminTable caption="Tenant teams with slug, namespace, and identifier." columns={COLUMNS} rows={teams} rowKey={(team) => team.id || team.slug} emptyMessage="No teams found." testId="teams-table" /></AsyncSection>
      <div className="toolbar"><div className="field grow"><label htmlFor="team-select">Manage members for</label><select id="team-select" value={selectedSlug} onChange={(event) => setSelectedSlug(event.target.value)} data-testid="team-select"><option value="">Select a team</option>{teams.map((team) => <option key={team.slug} value={team.slug}>{team.name || team.slug}</option>)}</select></div></div>
      <h3 className="subsection-title">Team members</h3>
      {membersLoading ? <p>Loading team members…</p> : <AdminTable caption="Members of the selected team." columns={memberColumns} rows={members} rowKey={(member) => member.user_id} emptyMessage={selectedSlug ? "No members in this team." : "Select a team."} testId="team-members-table" />}
    </section>
  );
}
