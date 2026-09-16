import { useMemo, useState } from "react";

import { ServerList } from "./ServerList";
import { ToolCatalog } from "./ToolCatalog";
import { ToolDetail } from "./ToolDetail";
import { EMPTY_FILTERS, ToolFilters, type CatalogFilters } from "./ToolFilters";
import { EmptyState } from "../EmptyState";
import { ErrorState } from "../ErrorState";
import { LoadingState } from "../LoadingState";
import { retireServer } from "../../api/catalog";
import { useCatalog } from "../../hooks/useCatalog";
import { formatPublishQuota, isServerReady, isTenantUser, serverKey, toolKey } from "../../api/types";
import type { AuthStatus, ServerSummary, ToolRow } from "../../api/types";

type ServersWorkspaceProps = {
  auth: AuthStatus;
  onSignIn: () => void;
};

export function matchesSearch(tool: ToolRow, search: string): boolean {
  const term = search.trim().toLowerCase();
  if (!term) {
    return true;
  }
  const haystack = [
    tool.tool_name,
    tool.description,
    tool.server_name,
    tool.namespace,
    tool.required_trust,
    tool.side_effect,
    tool.risk_level,
    tool.drift_status,
    ...Object.entries(tool.labels || {}).flat(),
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
  return haystack.includes(term);
}

export function filterServers(
  servers: ServerSummary[],
  filters: CatalogFilters
): ServerSummary[] {
  return servers.filter((server) => {
    if (filters.namespace && server.namespace !== filters.namespace) {
      return false;
    }
    if (filters.serverStatus === "ready" && !isServerReady(server)) {
      return false;
    }
    if (filters.serverStatus === "attention" && isServerReady(server)) {
      return false;
    }
    return true;
  });
}

export function filterTools(
  tools: ToolRow[],
  filters: CatalogFilters,
  visibleServerKeys: Set<string>
): ToolRow[] {
  return tools.filter((tool) => {
    const key = `${tool.namespace}/${tool.server_name}`;
    if (!visibleServerKeys.has(key)) {
      return false;
    }
    if (filters.selectedServerKey && key !== filters.selectedServerKey) {
      return false;
    }
    if (filters.risk && (tool.risk_level || "").toLowerCase() !== filters.risk) {
      return false;
    }
    return matchesSearch(tool, filters.search);
  });
}

export function ServersWorkspace({ auth, onSignIn }: ServersWorkspaceProps) {
  const authenticated = auth.authenticated;
  const [filters, setFilters] = useState<CatalogFilters>(EMPTY_FILTERS);
  const [selectedToolKey, setSelectedToolKey] = useState("");
  const catalog = useCatalog(authenticated, filters.namespace);

  const visibleServers = useMemo(
    () => filterServers(catalog.servers, filters),
    [catalog.servers, filters]
  );

  const visibleServerKeys = useMemo(
    () => new Set(visibleServers.map((server) => serverKey(server))),
    [visibleServers]
  );

  const toolCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const tool of catalog.tools) {
      const key = `${tool.namespace}/${tool.server_name}`;
      counts[key] = (counts[key] || 0) + 1;
    }
    return counts;
  }, [catalog.tools]);

  const scopedTools = useMemo(
    () => catalog.tools.filter((tool) => visibleServerKeys.has(`${tool.namespace}/${tool.server_name}`)),
    [catalog.tools, visibleServerKeys]
  );

  const visibleTools = useMemo(
    () => filterTools(catalog.tools, filters, visibleServerKeys),
    [catalog.tools, filters, visibleServerKeys]
  );

  const selectedTool = useMemo(
    () => visibleTools.find((tool) => toolKey(tool) === selectedToolKey),
    [visibleTools, selectedToolKey]
  );

  if (!authenticated) {
    return (
      <section className="panel" aria-labelledby="catalog-signed-out-title">
        <h2 id="catalog-signed-out-title">Server catalog</h2>
        <EmptyState
          title="Sign in to view the server catalog."
          detail="Namespaces, deployed MCP servers, and their governed tool inventory are scoped to your account."
          testId="catalog-signed-out"
          action={
            <button type="button" className="button primary" onClick={onSignIn}>
              Sign in
            </button>
          }
        />
      </section>
    );
  }

  if (catalog.status === "loading") {
    return <LoadingState label="Loading namespaces, servers, and tools…" testId="catalog-loading" />;
  }

  if (catalog.status === "unauthorized") {
    return (
      <ErrorState
        title="Your session expired."
        detail="Sign in again to reload the server catalog."
        onRetry={onSignIn}
        retryLabel="Sign in"
        testId="catalog-unauthorized"
      />
    );
  }

  if (catalog.status === "error") {
    return (
      <ErrorState
        title="The server catalog could not be loaded."
        detail={catalog.error}
        onRetry={catalog.reload}
        testId="catalog-error"
      />
    );
  }

  const readyCount = visibleServers.filter((server) => isServerReady(server)).length;

  return (
    <div className="servers-workspace">
      <section className="panel" aria-labelledby="server-catalog-title">
        <div className="panel-head">
          <div>
            <h2 id="server-catalog-title">Servers</h2>
            <p className="panel-lede">Browse deployed MCP endpoints and their tool inventory.</p>
          </div>
          <ul className="stat-row" aria-label="Server catalog summary" data-testid="server-stats">
            <li>
              <strong>{visibleServers.length}</strong> servers
            </li>
            <li>
              <strong>{readyCount}</strong> ready
            </li>
            <li>
              <strong>{scopedTools.length}</strong> tools
            </li>
            {isTenantUser(auth) ? (
              <li data-testid="server-quota">
                <strong>{formatPublishQuota(catalog.publishPolicy)}</strong> quota
              </li>
            ) : null}
          </ul>
        </div>
        <ToolFilters
          filters={filters}
          namespaces={catalog.namespaces}
          servers={visibleServers}
          onChange={(next) => {
            setFilters(next);
            setSelectedToolKey("");
          }}
          onReset={() => {
            setFilters(EMPTY_FILTERS);
            setSelectedToolKey("");
          }}
        />
        <ServerList
          servers={visibleServers}
          toolCounts={toolCounts}
          selectedKey={filters.selectedServerKey}
          onSelect={(key) => {
            setFilters((previous) => ({ ...previous, selectedServerKey: key }));
            setSelectedToolKey("");
          }}
          onRetire={async (namespace, name) => {
            await retireServer(namespace, name);
            catalog.reload();
          }}
        />
      </section>
      <ToolCatalog
        tools={visibleTools}
        totalCount={scopedTools.length}
        selectedToolKey={selectedToolKey}
        onSelectTool={setSelectedToolKey}
        emptyMessage={
          scopedTools.length === 0
            ? "No tools are published in this scope."
            : "No tools match these filters."
        }
      />
      {selectedTool ? (
        <ToolDetail tool={selectedTool} onClose={() => setSelectedToolKey("")} />
      ) : null}
    </div>
  );
}
