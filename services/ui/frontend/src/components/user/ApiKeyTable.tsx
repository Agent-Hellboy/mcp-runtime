import { useMemo } from "react";

import { Button } from "../../ui/Button";
import { StatusBadge } from "../../ui/Badge";
import { DataTable, buildColumns } from "../../ui/DataTable";
import { formatAbsolute, formatTimestamp } from "../../lib/format";
import type { UserAPIKey } from "../../api/types";

type ApiKeyTableProps = {
  keys: UserAPIKey[];
  revokingId: string;
  onRequestRevoke: (key: UserAPIKey) => void;
};

export function ApiKeyTable({ keys, revokingId, onRequestRevoke }: ApiKeyTableProps) {
  const columns = useMemo(
    () =>
      buildColumns<UserAPIKey>([
        {
          id: "name",
          header: "Name",
          rowHeader: true,
          sortValue: (item) => item.name || "",
          cell: (item) => item.name || "—",
        },
        {
          id: "prefix",
          header: "Prefix",
          cell: (item) => <code className="cell-code">{item.prefix}…</code>,
        },
        {
          id: "created",
          header: "Created",
          sortValue: (item) => Date.parse(item.created_at || "") || 0,
          cell: (item) => (
            <span title={formatAbsolute(item.created_at)}>{formatTimestamp(item.created_at)}</span>
          ),
        },
        {
          id: "status",
          header: "Status",
          sortValue: (item) => (item.revoked ? 1 : 0),
          cell: (item) => (
            <>
              <StatusBadge tone={item.revoked ? "attention" : "ready"}>
                {item.revoked ? "Revoked" : "Active"}
              </StatusBadge>
              {item.revoked && item.revoked_at ? (
                <span className="cell-detail">{formatTimestamp(item.revoked_at)}</span>
              ) : null}
            </>
          ),
        },
        {
          id: "actions",
          header: "Actions",
          cell: (item) =>
            item.revoked ? (
              <span className="cell-detail">No further action</span>
            ) : (
              <Button
                variant="danger-outline"
                size="sm"
                icon="trash"
                busy={revokingId === item.id}
                data-testid="revoke-key"
                onClick={() => onRequestRevoke(item)}
              >
                {revokingId === item.id ? "Revoking…" : "Revoke"}
              </Button>
            ),
        },
      ]),
    [revokingId, onRequestRevoke]
  );

  return (
    <DataTable
      columns={columns}
      rows={keys}
      rowKey={(item) => item.id}
      caption="Your API keys, with prefix, creation time, status, and a revoke action."
      regionLabel="API keys"
      testId="api-keys-table"
      rowTestId="api-key-row"
      emptyMessage="No keys match."
      pageSize={25}
    />
  );
}
