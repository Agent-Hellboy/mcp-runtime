import { useId } from "react";

import type { NamespaceEntry, ServerSummary } from "../../api/types";
import { serverKey } from "../../api/types";

export type ServerStatusFilter = "all" | "ready" | "attention";

export type CatalogFilters = {
  search: string;
  namespace: string;
  serverStatus: ServerStatusFilter;
  risk: string;
  selectedServerKey: string;
};

export const EMPTY_FILTERS: CatalogFilters = {
  search: "",
  namespace: "",
  serverStatus: "all",
  risk: "",
  selectedServerKey: "",
};

type ToolFiltersProps = {
  filters: CatalogFilters;
  namespaces: NamespaceEntry[];
  servers: ServerSummary[];
  onChange: (next: CatalogFilters) => void;
  onReset: () => void;
};

const STATUS_OPTIONS: Array<{ value: ServerStatusFilter; label: string }> = [
  { value: "all", label: "All" },
  { value: "ready", label: "Ready" },
  { value: "attention", label: "Issues" },
];

export function ToolFilters({
  filters,
  namespaces,
  servers,
  onChange,
  onReset,
}: ToolFiltersProps) {
  const searchId = useId();
  const namespaceId = useId();
  const riskId = useId();
  const serverId = useId();

  function patch(next: Partial<CatalogFilters>) {
    onChange({ ...filters, ...next });
  }

  return (
    <form
      className="toolbar"
      role="search"
      aria-label="Filter servers and tools"
      onSubmit={(event) => event.preventDefault()}
    >
      <div className="field grow">
        <label htmlFor={searchId}>Search tools</label>
        <input
          id={searchId}
          type="search"
          placeholder="Tool, server, namespace, or label"
          value={filters.search}
          onChange={(event) => patch({ search: event.target.value })}
          data-testid="tool-search"
        />
      </div>
      <div className="field">
        <label htmlFor={namespaceId}>Namespace</label>
        <select
          id={namespaceId}
          value={filters.namespace}
          onChange={(event) => patch({ namespace: event.target.value, selectedServerKey: "" })}
          data-testid="namespace-filter"
        >
          <option value="">All namespaces</option>
          {namespaces.map((entry) => (
            <option key={entry.namespace} value={entry.namespace}>
              {entry.namespace}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label htmlFor={serverId}>Server</label>
        <select
          id={serverId}
          value={filters.selectedServerKey}
          onChange={(event) => patch({ selectedServerKey: event.target.value })}
          data-testid="server-filter"
        >
          <option value="">All servers</option>
          {servers.map((server) => (
            <option key={serverKey(server)} value={serverKey(server)}>
              {server.name}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label htmlFor={riskId}>Risk</label>
        <select
          id={riskId}
          value={filters.risk}
          onChange={(event) => patch({ risk: event.target.value })}
          data-testid="tool-risk-filter"
        >
          <option value="">All risk</option>
          <option value="low">Low risk</option>
          <option value="medium">Medium risk</option>
          <option value="high">High risk</option>
        </select>
      </div>
      <fieldset className="segmented">
        <legend>Server status</legend>
        {STATUS_OPTIONS.map((option) => (
          <button
            key={option.value}
            type="button"
            className={filters.serverStatus === option.value ? "segment active" : "segment"}
            aria-pressed={filters.serverStatus === option.value}
            data-testid={`server-status-${option.value}`}
            onClick={() => patch({ serverStatus: option.value })}
          >
            {option.label}
          </button>
        ))}
      </fieldset>
      <button type="button" className="button ghost" onClick={onReset} data-testid="reset-filters">
        Reset
      </button>
    </form>
  );
}
