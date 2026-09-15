import { useState } from "react";

import { useAdminReload, useUsage } from "../../hooks/useAdminData";
import { AdminTable, type AdminColumn } from "./AdminTable";
import { AsyncSection } from "./AsyncSection";
import type { UsageResponse } from "../../api/types";

type UsageAnalyticsPanelProps = { onSignIn: () => void };

const SERVER_COLUMNS: Array<AdminColumn<UsageResponse["servers"][number]>> = [
  { id: "server", header: "Server", rowHeader: true, cell: (row) => row.server },
  { id: "namespace", header: "Namespace", cell: (row) => row.namespace },
  { id: "events", header: "Events", cell: (row) => String(row.events) },
  { id: "allowed", header: "Allow", cell: (row) => String(row.allowed) },
  { id: "denied", header: "Deny", cell: (row) => String(row.denied) },
];

const TOOL_COLUMNS: Array<AdminColumn<UsageResponse["tools"][number]>> = [
  { id: "server", header: "Server", rowHeader: true, cell: (row) => row.server },
  { id: "tool", header: "Tool", cell: (row) => row.tool_name },
  { id: "human", header: "Human", cell: (row) => row.human_id || "—" },
  { id: "agent", header: "Agent", cell: (row) => row.agent_id || "—" },
  { id: "events", header: "Calls", cell: (row) => String(row.events) },
  { id: "denied", header: "Denied", cell: (row) => String(row.denied) },
];

export function UsageAnalyticsPanel({ onSignIn }: UsageAnalyticsPanelProps) {
  const [limit, setLimit] = useState("10");
  const reload = useAdminReload();
  const query = useUsage(true, limit);
  const usage = query.data;
  return (
    <section className="panel" aria-labelledby="analytics-title">
      <div className="panel-head"><div><h2 id="analytics-title">Usage analytics</h2><p className="panel-lede">Gateway events aggregated by server, actor, tool, and decision.</p></div><div className="admin-links"><label className="field">Rows<select aria-label="Analytics row limit" value={limit} onChange={(event) => setLimit(event.target.value)}><option value="10">Top 10</option><option value="25">Top 25</option><option value="50">Top 50</option></select></label><button type="button" className="button ghost" onClick={reload}>Refresh</button></div></div>
      <AsyncSection query={query} loadingLabel="Loading usage analytics…" errorTitle="Analytics service could not be loaded." onRetry={reload} onSignIn={onSignIn} testId="analytics">
        <>
          <ul className="stat-row" aria-label="Analytics summary" data-testid="analytics-stats"><li><strong>{usage?.totals.events ?? 0}</strong> events</li><li><strong>{usage?.totals.allowed ?? 0}</strong> allowed</li><li><strong>{usage?.totals.denied ?? 0}</strong> denied</li><li><strong>{usage?.totals.unique_servers ?? 0}</strong> servers</li></ul>
          <h3 className="subsection-title">Servers</h3><AdminTable caption="Usage by MCP server." columns={SERVER_COLUMNS} rows={usage?.servers ?? []} rowKey={(row) => `${row.namespace}/${row.server}`} emptyMessage="No server usage yet." testId="analytics-servers-table" />
          <h3 className="subsection-title">Tools</h3><AdminTable caption="Usage by MCP tool." columns={TOOL_COLUMNS} rows={usage?.tools ?? []} rowKey={(row) => `${row.server}/${row.tool_name}/${row.agent_id}`} emptyMessage="No tool usage yet." testId="analytics-tools-table" />
        </>
      </AsyncSection>
    </section>
  );
}
