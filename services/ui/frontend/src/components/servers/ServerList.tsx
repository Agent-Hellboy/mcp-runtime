import { Button } from "../../ui/Button";
import { CopyButton } from "../../ui/CopyButton";
import { StatusBadge } from "../../ui/Badge";
import { EmptyState } from "../../ui/States";
import { formatAbsolute, formatAge } from "../../lib/format";
import { isServerReady, serverKey, type ServerSummary } from "../../api/types";

type ServerListProps = {
  servers: ServerSummary[];
  toolCounts: Record<string, number>;
  scopedServerKey: string;
  inspectedServerKey: string;
  onScope: (key: string) => void;
  onInspect: (key: string) => void;
  onClearFilters: () => void;
  filtered: boolean;
};

export function ServerList({
  servers,
  toolCounts,
  scopedServerKey,
  inspectedServerKey,
  onScope,
  onInspect,
  onClearFilters,
  filtered,
}: ServerListProps) {
  if (servers.length === 0) {
    return (
      <EmptyState
        icon="server"
        title={filtered ? "No servers match these filters." : "No MCP servers in this scope."}
        detail={
          filtered
            ? "Widen the namespace, status, or search filters to see more servers."
            : "Publish a server with `mcp-runtime server deploy`, or switch to a namespace that has one."
        }
        testId="server-list-empty"
        action={
          filtered ? (
            <Button variant="secondary" onClick={onClearFilters}>
              Clear filters
            </Button>
          ) : undefined
        }
      />
    );
  }

  return (
    <ul className="server-grid" data-testid="server-list">
      {servers.map((server) => {
        const key = serverKey(server);
        const ready = isServerReady(server);
        const scoped = key === scopedServerKey;
        const inspected = key === inspectedServerKey;
        const toolCount = toolCounts[key] ?? 0;

        return (
          <li key={key}>
            <article
              className={scoped || inspected ? "server-card is-selected" : "server-card"}
              data-testid="server-card"
              data-server-key={key}
            >
              <div className="server-card-head">
                <div>
                  <h3 className="server-card-name">{server.name}</h3>
                  <p className="server-card-namespace">{server.namespace}</p>
                </div>
                {/* Kubernetes readiness only: it says the replicas are up, not
                    that the MCP endpoint answered. */}
                <StatusBadge tone={ready ? "ready" : "attention"}>
                  {ready ? "Ready" : server.status || "Not ready"}
                </StatusBadge>
              </div>

              {server.description ? (
                <p className="server-card-description clamp-2" title={server.description}>
                  {server.description}
                </p>
              ) : null}

              <div className="server-card-facts">
                <span>
                  Replicas <b>{server.ready || "—"}</b>
                </span>
                <span>
                  Tools <b>{toolCount}</b>
                </span>
                {formatAge(server.age) ? (
                  <span title={formatAbsolute(server.age)}>
                    Age <b>{formatAge(server.age)}</b>
                  </span>
                ) : null}
                {server.authMode ? (
                  <span>
                    Auth <b>{server.authMode}</b>
                  </span>
                ) : null}
              </div>

              {server.endpoint ? (
                <span className="copy-row">
                  <span className="copy-value" title={server.endpoint}>
                    {server.endpoint}
                  </span>
                  <CopyButton
                    value={server.endpoint}
                    label={`Copy the endpoint for ${server.name}`}
                    testId="server-copy-endpoint"
                  />
                </span>
              ) : null}

              <div className="server-card-actions">
                <Button variant="secondary" size="sm" onClick={() => onInspect(key)} data-testid="server-card-details">
                  View details
                </Button>
                <Button
                  variant={scoped ? "primary" : "ghost"}
                  size="sm"
                  aria-pressed={scoped}
                  data-testid="server-card-select"
                  onClick={() => onScope(scoped ? "" : key)}
                >
                  {scoped ? "Clear server filter" : "Show tools"}
                </Button>
              </div>
            </article>
          </li>
        );
      })}
    </ul>
  );
}
