import { useState } from "react";

import { EmptyState } from "../EmptyState";
import { StatusBadge } from "../StatusBadge";
import {
  isServerReady,
  serverKey,
  serverPrompts,
  serverResources,
  serverTasks,
  type ServerSummary,
} from "../../api/types";

type ServerListProps = {
  servers: ServerSummary[];
  toolCounts: Record<string, number>;
  selectedKey: string;
  onSelect: (key: string) => void;
  // Omitted for a read-only view (the anonymous public-mode catalog preview)
  // where there is no session to retire anything with.
  onRetire?: (namespace: string, name: string) => Promise<void>;
};

async function copyConnectConfig(server: ServerSummary): Promise<void> {
  const json = JSON.stringify(server.access_json || {}, null, 2);
  await navigator.clipboard.writeText(json);
}

export function ServerList({ servers, toolCounts, selectedKey, onSelect, onRetire }: ServerListProps) {
  const [busyKey, setBusyKey] = useState("");
  const [error, setError] = useState("");
  const [copiedKey, setCopiedKey] = useState("");

  async function retire(server: ServerSummary): Promise<void> {
    if (
      !window.confirm(
        `Retire "${server.name}" in namespace "${server.namespace}"? This deletes the MCPServer and cannot be undone.`
      )
    ) {
      return;
    }
    const key = serverKey(server);
    if (!onRetire) {
      return;
    }
    setBusyKey(key);
    setError("");
    try {
      await onRetire(server.namespace, server.name);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Server retire failed.");
    } finally {
      setBusyKey("");
    }
  }

  async function copyConfig(server: ServerSummary): Promise<void> {
    const key = serverKey(server);
    setError("");
    try {
      await copyConnectConfig(server);
      setCopiedKey(key);
      window.setTimeout(() => setCopiedKey((current) => (current === key ? "" : current)), 2000);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Copying the connect config failed.");
    }
  }

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
    <>
      {error ? (
        <p className="inline-error" role="alert" data-testid="server-retire-error">
          {error}
        </p>
      ) : null}
      <ul className="server-grid" data-testid="server-list">
        {servers.map((server) => {
          const key = serverKey(server);
          const ready = isServerReady(server);
          const selected = key === selectedKey;
          const toolCount = toolCounts[key] ?? 0;
          const busy = busyKey === key;
          const prompts = serverPrompts(server);
          const resources = serverResources(server);
          const tasks = serverTasks(server);
          const hasConnectConfig = Boolean(server.access_json && Object.keys(server.access_json).length);
          const observability = server.observability;
          const hasObservability = Boolean(
            observability && (observability.grafana.available || observability.prometheus.queries.length)
          );
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
                  {prompts.length ? (
                    <div>
                      <dt>Prompts</dt>
                      <dd>{prompts.length}</dd>
                    </div>
                  ) : null}
                  {resources.length ? (
                    <div>
                      <dt>Resources</dt>
                      <dd>{resources.length}</dd>
                    </div>
                  ) : null}
                  {tasks.length ? (
                    <div>
                      <dt>Tasks</dt>
                      <dd>{tasks.length}</dd>
                    </div>
                  ) : null}
                </dl>
                {server.description ? (
                  <p className="server-card-description">{server.description}</p>
                ) : null}
                {server.endpoint ? (
                  <p className="server-card-endpoint" title={server.endpoint}>
                    {server.endpoint}
                  </p>
                ) : null}
                {prompts.length || resources.length || tasks.length ? (
                  <details className="server-card-inventory" data-testid="server-card-inventory">
                    <summary>Protocol inventory</summary>
                    {prompts.length ? (
                      <p>
                        <strong>Prompts:</strong> {prompts.join(", ")}
                      </p>
                    ) : null}
                    {resources.length ? (
                      <p>
                        <strong>Resources:</strong> {resources.join(", ")}
                      </p>
                    ) : null}
                    {tasks.length ? (
                      <p>
                        <strong>Tasks:</strong> {tasks.join(", ")}
                      </p>
                    ) : null}
                  </details>
                ) : null}
                {hasObservability ? (
                  <div className="server-card-observability" data-testid="server-card-observability">
                    {observability?.grafana.available && observability.grafana.url ? (
                      <a
                        className="button ghost"
                        href={observability.grafana.url}
                        target="_blank"
                        rel="noreferrer"
                        data-testid="server-card-grafana-link"
                      >
                        Grafana dashboard
                      </a>
                    ) : null}
                    {observability?.prometheus.queries.map((linkItem) => (
                      <a
                        key={linkItem.id}
                        className="button ghost"
                        href={linkItem.url}
                        target="_blank"
                        rel="noreferrer"
                        title={linkItem.description}
                      >
                        {linkItem.name}
                      </a>
                    ))}
                  </div>
                ) : null}
                <div className="server-card-actions">
                  <button
                    type="button"
                    className={selected ? "button primary" : "button ghost"}
                    aria-pressed={selected}
                    data-testid="server-card-select"
                    onClick={() => onSelect(selected ? "" : key)}
                  >
                    {selected ? "Clear server filter" : "Show tools"}
                  </button>
                  {hasConnectConfig ? (
                    <button
                      type="button"
                      className="button ghost"
                      data-testid="server-card-copy-connect-config"
                      onClick={() => void copyConfig(server)}
                    >
                      {copiedKey === key ? "Copied" : "Copy connect config"}
                    </button>
                  ) : null}
                  {onRetire ? (
                    <button
                      type="button"
                      className="button ghost danger"
                      data-testid="server-card-retire"
                      disabled={busy}
                      onClick={() => void retire(server)}
                    >
                      {busy ? "Retiring…" : "Retire"}
                    </button>
                  ) : null}
                </div>
              </article>
            </li>
          );
        })}
      </ul>
    </>
  );
}
