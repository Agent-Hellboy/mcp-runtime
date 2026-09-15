import { EmptyState } from "../EmptyState";
import { StatusBadge } from "../StatusBadge";
import type { UserAPIKey } from "../../api/types";

type ApiKeyTableProps = {
  keys: UserAPIKey[];
  revokingId: string;
  pendingRevokeId: string;
  onRequestRevoke: (id: string) => void;
  onConfirmRevoke: (id: string) => void;
  onCancelRevoke: () => void;
};

function formatDate(value: string | undefined): string {
  if (!value) {
    return "—";
  }
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? "—" : parsed.toLocaleString();
}

export function ApiKeyTable({
  keys,
  revokingId,
  pendingRevokeId,
  onRequestRevoke,
  onConfirmRevoke,
  onCancelRevoke,
}: ApiKeyTableProps) {
  if (keys.length === 0) {
    return (
      <EmptyState
        title="You have no API keys yet."
        detail="Create one to authenticate an agent or CI job against MCP Runtime."
        testId="api-keys-empty"
      />
    );
  }

  return (
    <div className="table-scroll" data-testid="api-keys-table-scroll" tabIndex={0}>
      <table className="data-table" data-testid="api-keys-table">
        <caption className="visually-hidden">
          Your API keys, with prefix, creation time, status, and a revoke action.
        </caption>
        <thead>
          <tr>
            <th scope="col">Name</th>
            <th scope="col">Prefix</th>
            <th scope="col">Created</th>
            <th scope="col">Status</th>
            <th scope="col">Actions</th>
          </tr>
        </thead>
        <tbody>
          {keys.map((item) => {
            const busy = revokingId === item.id;
            const confirming = pendingRevokeId === item.id;
            return (
              <tr key={item.id} data-testid="api-key-row">
                <th scope="row">{item.name || "—"}</th>
                <td>
                  <code>{item.prefix}…</code>
                </td>
                <td>{formatDate(item.created_at)}</td>
                <td>
                  <StatusBadge tone={item.revoked ? "attention" : "ready"}>
                    {item.revoked ? "Revoked" : "Active"}
                  </StatusBadge>
                </td>
                <td>
                  {item.revoked ? (
                    <span className="cell-detail">Revoked {formatDate(item.revoked_at)}</span>
                  ) : confirming ? (
                    // Revocation is irreversible, so it takes a second, explicit
                    // confirmation naming the key being revoked.
                    <span className="confirm-inline" data-testid="revoke-confirm">
                      <span className="cell-detail">Revoke {item.name}?</span>
                      <button
                        type="button"
                        className="button primary"
                        disabled={busy}
                        data-testid="revoke-confirm-yes"
                        onClick={() => onConfirmRevoke(item.id)}
                      >
                        {busy ? "Revoking…" : "Confirm"}
                      </button>
                      <button
                        type="button"
                        className="button ghost"
                        disabled={busy}
                        data-testid="revoke-confirm-cancel"
                        onClick={onCancelRevoke}
                      >
                        Cancel
                      </button>
                    </span>
                  ) : (
                    <button
                      type="button"
                      className="button ghost"
                      disabled={busy}
                      data-testid="revoke-key"
                      onClick={() => onRequestRevoke(item.id)}
                    >
                      Revoke
                    </button>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
