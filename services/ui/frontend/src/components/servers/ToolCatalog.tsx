import { flexRender, useTable, type SortingState } from "@tanstack/react-table";
import { useMemo, useState } from "react";

import { buildToolColumns, toolTableFeatures } from "./toolColumns";
import type { ToolRow } from "../../api/types";

type ToolCatalogProps = {
  tools: ToolRow[];
  totalCount: number;
  selectedToolKey: string;
  onSelectTool: (key: string) => void;
  emptyMessage: string;
};

function sortHint(direction: false | "asc" | "desc"): string {
  if (direction === "asc") return "ascending";
  if (direction === "desc") return "descending";
  return "none";
}

export function ToolCatalog({
  tools,
  totalCount,
  selectedToolKey,
  onSelectTool,
  emptyMessage,
}: ToolCatalogProps) {
  const [sorting, setSorting] = useState<SortingState>([]);

  const columns = useMemo(
    () => buildToolColumns({ selectedToolKey, onSelectTool }),
    [selectedToolKey, onSelectTool]
  );

  const table = useTable({
    features: toolTableFeatures,
    columns,
    data: tools,
    state: { sorting },
    onSortingChange: setSorting,
  });

  const rows = table.getRowModel().rows;
  const caption =
    tools.length === totalCount
      ? `${totalCount} tool${totalCount === 1 ? "" : "s"}`
      : `${tools.length} of ${totalCount} tools`;

  return (
    <section className="panel tool-catalog" aria-labelledby="tool-catalog-title">
      <div className="panel-head">
        <h2 id="tool-catalog-title">Tools</h2>
        <p className="panel-count" data-testid="catalog-summary" aria-live="polite">
          {caption}
        </p>
      </div>
      <div className="table-scroll" data-testid="tool-table-scroll" tabIndex={0}>
        <table className="data-table" data-testid="tool-table">
          <caption className="visually-hidden">
            Governed tool catalog with trust, side effect, risk, and drift for each tool. Column
            headers are buttons that sort the table.
          </caption>
          <thead>
            {table.getHeaderGroups().map((group) => (
              <tr key={group.id}>
                {group.headers.map((header) => {
                  const direction = header.column.getIsSorted();
                  return (
                    <th
                      key={header.id}
                      scope="col"
                      aria-sort={
                        direction === "asc"
                          ? "ascending"
                          : direction === "desc"
                            ? "descending"
                            : "none"
                      }
                    >
                      <button
                        type="button"
                        className="column-sort"
                        data-testid={`tool-sort-${header.column.id}`}
                        onClick={header.column.getToggleSortingHandler()}
                      >
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        <span className="visually-hidden">
                          , sorted {sortHint(direction)}, activate to change sorting
                        </span>
                        <span className="sort-indicator" aria-hidden="true">
                          {direction === "asc" ? "▲" : direction === "desc" ? "▼" : "↕"}
                        </span>
                      </button>
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr>
                <td colSpan={columns.length} className="table-empty" data-testid="tool-table-empty">
                  {emptyMessage}
                </td>
              </tr>
            ) : (
              rows.map((row) => {
                const tool = row.original;
                const key = `${tool.namespace}/${tool.server_name}/${tool.tool_name}`;
                const cells = row.getAllCells();
                const [first, ...rest] = cells;
                return (
                  <tr
                    key={key}
                    className={key === selectedToolKey ? "selected" : undefined}
                    data-testid="tool-row"
                  >
                    <th scope="row">
                      {flexRender(first.column.columnDef.cell, first.getContext())}
                    </th>
                    {rest.map((cell) => (
                      <td key={cell.id}>
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}
