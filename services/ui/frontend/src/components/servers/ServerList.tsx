import { EmptyState } from "../EmptyState";
import { StatusBadge } from "../StatusBadge";
import { isServerReady, serverKey, type ServerSummary } from "../../api/types";

type ServerListProps = {
  servers: ServerSummary[];
  toolCounts: Record<string, number>;
  selectedKey: string;
  onSelect: (key: string) => void;
};

export function ServerList({ servers, toolCounts, selectedKey, onSelect }: ServerListProps) {
  if (servers.length === 0) {
    return (
      <EmptyState
        title="No MCP servers match this scope."
        detail="Publish a server, or widen the namespace and status filters."
        testId="server-list-empty"
      />
    );
  }

  return (
    <ul className="server-grid" data-testid="server-list">
      {servers.map((server) => {
        const key = serverKey(server);
        const ready = isServerReady(server);
        const selected = key === selectedKey;
        const toolCount = toolCounts[key] ?? 0;
        return (
          <li key={key}>
            <article
              className={selected ? "server-card selected" : "server-card"}
              data-testid="server-card"
              data-server-key={key}
            >
              <header className="server-card-head">
                <h3>{server.name}</h3>
                <StatusBadge tone={ready ? "ready" : "attention"}>
                  {ready ? "Ready" : server.status || "Not ready"}
                </StatusBadge>
              </header>
              <dl className="server-card-meta">
                <div>
                  <dt>Namespace</dt>
                  <dd>{server.namespace}</dd>
                </div>
                <div>
                  <dt>Replicas</dt>
                  <dd>{server.ready || "—"}</dd>
                </div>
                <div>
                  <dt>Tools</dt>
                  <dd>{toolCount}</dd>
                </div>
              </dl>
              {server.description ? (
                <p className="server-card-description">{server.description}</p>
              ) : null}
              {server.endpoint ? (
                <p className="server-card-endpoint" title={server.endpoint}>
                  {server.endpoint}
                </p>
              ) : null}
              <button
                type="button"
                className={selected ? "button primary" : "button ghost"}
                aria-pressed={selected}
                data-testid="server-card-select"
                onClick={() => onSelect(selected ? "" : key)}
              >
                {selected ? "Clear server filter" : "Show tools"}
              </button>
            </article>
          </li>
        );
      })}
    </ul>
  );
}
