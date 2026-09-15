import { AdminTable, type AdminColumn } from "./AdminTable";
import { AsyncSection } from "./AsyncSection";
import { useAdminReload, useTeams } from "../../hooks/useAdminData";
import type { TeamRecord } from "../../api/types";

type TeamsPanelProps = {
  onSignIn: () => void;
};

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

  return (
    <section className="panel" aria-labelledby="teams-title">
      <div className="panel-head">
        <div>
          <h2 id="teams-title">Teams</h2>
          <p className="panel-lede">Tenant teams and the namespace each one owns.</p>
        </div>
        <ul className="stat-row" aria-label="Team summary" data-testid="teams-stats">
          <li>
            <strong>{teams.length}</strong> teams
          </li>
        </ul>
      </div>
      <AsyncSection
        query={teamsQuery}
        loadingLabel="Loading teams…"
        errorTitle="Teams could not be loaded."
        onRetry={reload}
        onSignIn={onSignIn}
        testId="teams"
      >
        <AdminTable
          caption="Tenant teams with slug, namespace, and identifier."
          columns={COLUMNS}
          rows={teams}
          rowKey={(team) => team.id || team.slug}
          emptyMessage="No teams found."
          testId="teams-table"
        />
      </AsyncSection>
      <p className="panel-footnote">
        Creating, renaming, and deleting teams, and editing membership, stay in the legacy
        dashboard under More workspaces.
      </p>
    </section>
  );
}
