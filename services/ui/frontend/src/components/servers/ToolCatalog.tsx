import { useMemo } from "react";

import { buildToolColumns } from "./toolColumns";
import type { ToolFilters } from "./filters";
import { Button } from "../../ui/Button";
import { DataTable } from "../../ui/DataTable";
import { TextField, SelectField } from "../../ui/Field";
import { FilterBar, FilterSummary, type FilterChip } from "../../ui/FilterBar";
import { toolKey, type ToolRow } from "../../api/types";

const RISK_OPTIONS = [
  { value: "", label: "All risk levels" },
  { value: "low", label: "Low risk" },
  { value: "medium", label: "Medium risk" },
  { value: "high", label: "High risk" },
];

// The three values services/runtime-api can return for drift_status.
const DRIFT_OPTIONS = [
  { value: "", label: "All drift states" },
  { value: "declared", label: "Declared" },
  { value: "missing", label: "Missing" },
  { value: "ungoverned", label: "Ungoverned" },
];

type ToolCatalogProps = {
  tools: ToolRow[];
  scopedCount: number;
  filters: ToolFilters;
  scopedServerName: string;
  selectedToolKey: string;
  onFiltersChange: (next: ToolFilters) => void;
  onClearFilters: () => void;
  onSelectTool: (key: string) => void;
};

export function ToolCatalog({
  tools,
  scopedCount,
  filters,
  scopedServerName,
  selectedToolKey,
  onFiltersChange,
  onClearFilters,
  onSelectTool,
}: ToolCatalogProps) {
  const columns = useMemo(
    () => buildToolColumns({ selectedToolKey, onSelectTool }),
    [selectedToolKey, onSelectTool]
  );

  function patch(next: Partial<ToolFilters>) {
    onFiltersChange({ ...filters, ...next });
  }

  const chips: FilterChip[] = [];
  if (filters.search.trim()) {
    chips.push({ id: "search", label: "Search", value: filters.search.trim(), onRemove: () => patch({ search: "" }) });
  }
  if (filters.serverKey) {
    chips.push({
      id: "server",
      label: "Server",
      value: scopedServerName || filters.serverKey,
      onRemove: () => patch({ serverKey: "" }),
    });
  }
  if (filters.risk) {
    chips.push({ id: "risk", label: "Risk", value: filters.risk, onRemove: () => patch({ risk: "" }) });
  }
  if (filters.drift) {
    chips.push({ id: "drift", label: "Drift", value: filters.drift, onRemove: () => patch({ drift: "" }) });
  }

  const count =
    tools.length === scopedCount
      ? `${scopedCount} tool${scopedCount === 1 ? "" : "s"}`
      : `${tools.length} of ${scopedCount} tools`;

  return (
    <section className="section">
      <div className="section-head">
        <h2 className="section-title" id="tool-catalog-title">
          Tools
        </h2>
        <p className="section-note">
          Governance metadata is declared by each server and evaluated by the gateway.
        </p>
      </div>

      <FilterBar label="Filter tools">
        <TextField
          label="Search tools"
          type="search"
          fieldClassName="grow"
          leadingIcon
          placeholder="Tool name, description, governance field, or label"
          value={filters.search}
          data-testid="tool-search"
          onChange={(event) => patch({ search: event.target.value })}
        />
        <SelectField
          label="Risk"
          value={filters.risk}
          options={RISK_OPTIONS}
          data-testid="tool-risk-filter"
          onChange={(event) => patch({ risk: event.target.value })}
        />
        <SelectField
          label="Drift"
          value={filters.drift}
          options={DRIFT_OPTIONS}
          data-testid="tool-drift-filter"
          onChange={(event) => patch({ drift: event.target.value })}
        />
      </FilterBar>

      <FilterSummary chips={chips} count={count} onClear={onClearFilters} testId="catalog-summary" />

      <DataTable
        columns={columns}
        rows={tools}
        rowKey={toolKey}
        caption="Governed tool catalog with trust, side effect, risk, and drift for each tool. Column headers are buttons that sort the table."
        regionLabel="Tool catalog"
        testId="tool-table"
        rowTestId="tool-row"
        sortTestIdPrefix="tool-sort"
        selectedKey={selectedToolKey}
        emptyMessage={
          scopedCount === 0 ? "No tools are published in this scope." : "No tools match these filters."
        }
        pageSize={25}
      />

      {chips.length > 0 && tools.length === 0 && scopedCount > 0 ? (
        <div className="filter-summary">
          <Button variant="secondary" size="sm" onClick={onClearFilters}>
            Clear tool filters
          </Button>
        </div>
      ) : null}
    </section>
  );
}
