import { useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";

import { TeamMembershipPanel } from "./TeamMembershipPanel";
import { UsageSummary } from "./UsageSummary";
import { ServerUsageTable, ToolUsageTable } from "./UsageTables";
import { EmptyState } from "../EmptyState";
import { ErrorState } from "../ErrorState";
import { LoadingState } from "../LoadingState";
import { UnauthorizedError } from "../../api/client";
import { listTeams, readUserUsage } from "../../api/userWorkflows";
import type { AuthStatus } from "../../api/types";
import { isTenantUser } from "../../api/types";

const WINDOW_OPTIONS = [
  { value: 1, label: "24 hours" },
  { value: 7, label: "7 days" },
  { value: 30, label: "30 days" },
  { value: 90, label: "90 days" },
];

type ActivityWorkspaceProps = {
  auth: AuthStatus;
  onSignIn: () => void;
};

export function ActivityWorkspace({ auth, onSignIn }: ActivityWorkspaceProps) {
  const [windowDays, setWindowDays] = useState(7);
  const [server, setServer] = useState("");
  const windowId = useId();
  const serverId = useId();

  // Legacy data-user-only gate: Activity is the tenant view. Admins use the
  // org-wide Analytics surface instead.
  const enabled = isTenantUser(auth);

  const usageQuery = useQuery({
    queryKey: ["user", "usage", windowDays, server],
    queryFn: () => readUserUsage(windowDays, server || undefined),
    enabled,
  });

  const teamsQuery = useQuery({
    queryKey: ["user", "teams"],
    queryFn: listTeams,
    enabled,
  });

  if (!auth.authenticated) {
    return (
      <section className="panel" aria-labelledby="activity-signed-out-title">
        <h2 id="activity-signed-out-title">My activity</h2>
        <EmptyState
          title="Sign in to see your activity."
          detail="Usage is scoped to the MCP servers in your user and team namespaces."
          testId="activity-signed-out"
          action={
            <button type="button" className="button primary" onClick={onSignIn}>
              Sign in
            </button>
          }
        />
      </section>
    );
  }

  if (!enabled) {
    return (
      <section className="panel" aria-labelledby="activity-admin-title">
        <h2 id="activity-admin-title">My activity</h2>
        <EmptyState
          title="This view is for tenant accounts."
          detail="Administrators see org-wide usage in the Analytics workspace instead."
          testId="activity-admin-hidden"
        />
      </section>
    );
  }

  if (usageQuery.isPending) {
    return <LoadingState label="Loading your activity…" testId="activity-loading" />;
  }

  if (usageQuery.error instanceof UnauthorizedError) {
    return (
      <ErrorState
        title="Your session expired."
        detail="Sign in again to see your activity."
        onRetry={onSignIn}
        retryLabel="Sign in"
        testId="activity-unauthorized"
      />
    );
  }

  if (usageQuery.error) {
    return (
      <ErrorState
        title="Your activity could not be loaded."
        detail={
          usageQuery.error instanceof Error
            ? usageQuery.error.message
            : "The analytics service did not respond."
        }
        onRetry={() => void usageQuery.refetch()}
        testId="activity-error"
      />
    );
  }

  const usage = usageQuery.data;
  const servers = usage?.servers ?? [];
  const tools = usage?.tools ?? [];

  return (
    <div className="user-workspace">
      <section className="panel" aria-labelledby="activity-title">
        <div className="panel-head">
          <div>
            <h2 id="activity-title">My MCP activity</h2>
            <p className="panel-lede">
              Usage for MCP servers in your user and team namespaces.
            </p>
          </div>
          <form className="toolbar inline" onSubmit={(event) => event.preventDefault()}>
            <div className="field">
              <label htmlFor={serverId}>Server</label>
              <select
                id={serverId}
                value={server}
                onChange={(event) => setServer(event.target.value)}
                data-testid="activity-server-filter"
              >
                <option value="">All servers</option>
                {servers.map((row) => (
                  <option key={`${row.namespace}/${row.server}`} value={row.server}>
                    {row.server}
                  </option>
                ))}
              </select>
            </div>
            <div className="field">
              <label htmlFor={windowId}>Time range</label>
              <select
                id={windowId}
                value={String(windowDays)}
                onChange={(event) => setWindowDays(Number(event.target.value))}
                data-testid="activity-window-filter"
              >
                {WINDOW_OPTIONS.map((option) => (
                  <option key={option.value} value={String(option.value)}>
                    {option.label}
                  </option>
                ))}
              </select>
            </div>
          </form>
        </div>

        {usage ? <UsageSummary totals={usage.totals} /> : null}

        <h3 className="section-heading">Servers</h3>
        <ServerUsageTable rows={servers} />

        <h3 className="section-heading">Tools</h3>
        <ToolUsageTable rows={tools} />
      </section>

      {teamsQuery.isPending ? (
        <LoadingState label="Loading your teams…" testId="teams-loading" />
      ) : teamsQuery.error ? (
        <ErrorState
          title="Your team membership could not be loaded."
          detail={teamsQuery.error instanceof Error ? teamsQuery.error.message : ""}
          onRetry={() => void teamsQuery.refetch()}
          testId="teams-error"
        />
      ) : (
        <TeamMembershipPanel teams={teamsQuery.data ?? []} />
      )}
    </div>
  );
}
