import type { ServerUsage, ToolUsage } from "../../api/types";

function formatDate(value: string | undefined): string {
  if (!value) {
    return "—";
  }
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? "—" : parsed.toLocaleString();
}

export function ServerUsageTable({ rows }: { rows: ServerUsage[] }) {
  return (
    <div className="table-scroll" data-testid="server-usage-scroll" tabIndex={0}>
      <table className="data-table" data-testid="server-usage-table">
        <caption className="visually-hidden">
          MCP servers in your namespaces, with request, allow, and deny counts.
        </caption>
        <thead>
          <tr>
            <th scope="col">Server</th>
            <th scope="col">Namespace</th>
            <th scope="col">Requests</th>
            <th scope="col">Allowed</th>
            <th scope="col">Denied</th>
            <th scope="col">Last seen</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={6} className="table-empty" data-testid="server-usage-empty">
                No server activity in this window.
              </td>
            </tr>
          ) : (
            rows.map((row) => (
              <tr key={`${row.namespace}/${row.server}`} data-testid="server-usage-row">
                <th scope="row">{row.server}</th>
                <td>{row.namespace}</td>
                <td>{row.events}</td>
                <td>{row.allowed}</td>
                <td>{row.denied}</td>
                <td>{formatDate(row.last_seen)}</td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}

export function ToolUsageTable({ rows }: { rows: ToolUsage[] }) {
  return (
    <div className="table-scroll" data-testid="tool-usage-scroll" tabIndex={0}>
      <table className="data-table" data-testid="tool-usage-table">
        <caption className="visually-hidden">
          Tools you invoked, with request and deny counts per server.
        </caption>
        <thead>
          <tr>
            <th scope="col">Tool</th>
            <th scope="col">Server</th>
            <th scope="col">Requests</th>
            <th scope="col">Denied</th>
            <th scope="col">Last seen</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={5} className="table-empty" data-testid="tool-usage-empty">
                No tool activity in this window.
              </td>
            </tr>
          ) : (
            rows.map((row) => (
              <tr key={`${row.server}/${row.tool_name}`} data-testid="tool-usage-row">
                <th scope="row">{row.tool_name}</th>
                <td>{row.server}</td>
                <td>{row.events}</td>
                <td>{row.denied}</td>
                <td>{formatDate(row.last_seen)}</td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}
