import {
  createColumnHelper,
  createSortedRowModel,
  rowSortingFeature,
  sortFns,
  tableFeatures,
} from "@tanstack/react-table";

import { StatusBadge, riskTone } from "../StatusBadge";
import type { ToolRow } from "../../api/types";

// Only the features this table uses are registered, so the rest tree-shakes out.
export const toolTableFeatures = tableFeatures({
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
  sortFns,
});

const helper = createColumnHelper<typeof toolTableFeatures, ToolRow>();

// Risk and trust are ordinal, not alphabetical: sorting them as text would put
// "high" before "low". Rank them explicitly instead.
const RISK_ORDER: Record<string, number> = { low: 1, medium: 2, high: 3 };
const TRUST_ORDER: Record<string, number> = { low: 1, medium: 2, high: 3 };

function rank(order: Record<string, number>, value: unknown): number {
  return order[String(value || "").toLowerCase()] ?? 0;
}

function driftTone(drift: string): "ready" | "attention" | "neutral" {
  switch (drift) {
    case "declared":
      return "ready";
    case "missing":
    case "ungoverned":
      return "attention";
    default:
      return "neutral";
  }
}

type ToolColumnOptions = {
  selectedToolKey: string;
  onSelectTool: (key: string) => void;
};

export function buildToolColumns({ selectedToolKey, onSelectTool }: ToolColumnOptions) {
  return helper.columns([
    helper.accessor("tool_name", {
      id: "tool_name",
      header: "Tool",
      sortFn: "alphanumeric",
      cell: ({ row }) => {
        const tool = row.original;
        const key = `${tool.namespace}/${tool.server_name}/${tool.tool_name}`;
        const selected = key === selectedToolKey;
        return (
          <>
            <button
              type="button"
              className="link-button"
              aria-pressed={selected}
              data-testid="tool-row-select"
              onClick={() => onSelectTool(selected ? "" : key)}
            >
              {tool.tool_name}
            </button>
            {tool.description ? <span className="cell-detail">{tool.description}</span> : null}
          </>
        );
      },
    }),
    helper.accessor("server_name", {
      id: "server_name",
      header: "Server",
      sortFn: "alphanumeric",
      cell: ({ row }) => (
        <>
          {row.original.server_name}
          <span className="cell-detail">{row.original.namespace}</span>
        </>
      ),
    }),
    helper.accessor((tool) => tool.required_trust || "", {
      id: "required_trust",
      header: "Trust",
      sortFn: (a, b) =>
        rank(TRUST_ORDER, a.original.required_trust) - rank(TRUST_ORDER, b.original.required_trust),
      cell: ({ row }) => row.original.required_trust || "—",
    }),
    helper.accessor((tool) => tool.side_effect || "", {
      id: "side_effect",
      header: "Side effect",
      sortFn: "alphanumeric",
      cell: ({ row }) => row.original.side_effect || "—",
    }),
    helper.accessor((tool) => tool.risk_level || "", {
      id: "risk_level",
      header: "Risk",
      sortFn: (a, b) =>
        rank(RISK_ORDER, a.original.risk_level) - rank(RISK_ORDER, b.original.risk_level),
      cell: ({ row }) => (
        <StatusBadge tone={riskTone(row.original.risk_level)}>
          {row.original.risk_level || "unrated"}
        </StatusBadge>
      ),
    }),
    helper.accessor("drift_status", {
      id: "drift_status",
      header: "Drift",
      sortFn: "alphanumeric",
      cell: ({ row }) => (
        <StatusBadge tone={driftTone(row.original.drift_status)}>
          {row.original.drift_status || "unknown"}
        </StatusBadge>
      ),
    }),
  ]);
}
