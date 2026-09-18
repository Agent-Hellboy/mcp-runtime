import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { ApiKeyTable } from "./ApiKeyTable";
import { CreateKeyForm } from "./CreateKeyForm";
import { OneTimeKeyNotice } from "./OneTimeKeyNotice";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { Icon } from "../../ui/Icon";
import { PageHeader } from "../../ui/PageHeader";
import { EmptyState, ErrorState, LoadingState } from "../../ui/States";
import { Button } from "../../ui/Button";
import { CSRFError, UnauthorizedError } from "../../api/client";
import { createUserAPIKey, listUserAPIKeys, revokeUserAPIKey } from "../../api/userWorkflows";
import type { AuthStatus, UserAPIKey } from "../../api/types";
import { hasUserIdentity } from "../../api/types";

export const USER_KEYS_QUERY_KEY = ["user", "api-keys"];

type ApiKeysWorkspaceProps = {
  auth: AuthStatus;
  onSignIn: () => void;
};

export function mutationErrorMessage(err: unknown): string {
  if (err instanceof CSRFError) {
    return "Your session security token expired. Reload the page and try again.";
  }
  if (err instanceof UnauthorizedError) {
    return "Your session expired. Sign in again to continue.";
  }
  const raw = err instanceof Error ? err.message : "";
  if (raw.includes("forbidden")) {
    return "You do not have permission to do that.";
  }
  if (raw.includes("name required") || raw.includes("invalid request body")) {
    return "That name was rejected. Enter a different name.";
  }
  return raw.trim() || "The request failed. Try again.";
}

export function ApiKeysWorkspace({ auth, onSignIn }: ApiKeysWorkspaceProps) {
  const queryClient = useQueryClient();
  // Cleartext key material lives only here, and only until dismissed.
  const [oneTimeKey, setOneTimeKey] = useState<{ name: string; value: string } | null>(null);
  const [pendingRevoke, setPendingRevoke] = useState<UserAPIKey | null>(null);
  const [notice, setNotice] = useState("");
  const [revokeError, setRevokeError] = useState("");

  const enabled = hasUserIdentity(auth);

  const keysQuery = useQuery({
    queryKey: USER_KEYS_QUERY_KEY,
    queryFn: listUserAPIKeys,
    enabled,
  });

  const createMutation = useMutation({
    mutationFn: createUserAPIKey,
    onSuccess: (created, name) => {
      setNotice("");
      if (created.oneTimeKey) {
        setOneTimeKey({ name, value: created.oneTimeKey });
      }
      void queryClient.invalidateQueries({ queryKey: USER_KEYS_QUERY_KEY });
    },
  });

  const revokeMutation = useMutation({
    mutationFn: revokeUserAPIKey,
    onSuccess: (_result, id) => {
      const name = keysQuery.data?.find((item) => item.id === id)?.name;
      setPendingRevoke(null);
      setRevokeError("");
      setNotice(name ? `Key "${name}" revoked. It can no longer authenticate.` : "Key revoked.");
      void queryClient.invalidateQueries({ queryKey: USER_KEYS_QUERY_KEY });
    },
    onError: (error) => {
      setPendingRevoke(null);
      setNotice("");
      setRevokeError(mutationErrorMessage(error));
    },
  });

  if (!auth.authenticated) {
    return (
      <>
        <PageHeader title="API keys" />
        <EmptyState
          icon="key"
          title="Sign in to manage your API keys."
          detail="Keys are scoped to your platform account."
          testId="keys-signed-out"
          action={
            <Button variant="primary" icon="login" onClick={onSignIn}>
              Sign in
            </Button>
          }
        />
      </>
    );
  }

  if (!enabled) {
    // A session without a
    // user subject (for example UI API-key login) owns no personal keys.
    return (
      <>
        <PageHeader title="API keys" />
        <EmptyState
          icon="key"
          title="This session has no user identity."
          detail="Personal API keys are available when you sign in with a platform account."
          testId="keys-no-identity"
        />
      </>
    );
  }

  if (keysQuery.isPending) {
    return (
      <>
        <PageHeader title="API keys" />
        <LoadingState label="Loading your API keys…" testId="keys-loading" />
      </>
    );
  }

  if (keysQuery.error instanceof UnauthorizedError) {
    return (
      <>
        <PageHeader title="API keys" />
        <ErrorState
          title="Your session expired."
          detail="Sign in again to manage your API keys."
          onRetry={onSignIn}
          retryLabel="Sign in"
          testId="keys-unauthorized"
        />
      </>
    );
  }

  if (keysQuery.error) {
    return (
      <>
        <PageHeader title="API keys" />
        <ErrorState
          title="Your API keys could not be loaded."
          detail={mutationErrorMessage(keysQuery.error)}
          onRetry={() => void keysQuery.refetch()}
          testId="keys-error"
        />
      </>
    );
  }

  const keys = keysQuery.data ?? [];
  const activeCount = keys.filter((key) => !key.revoked).length;
  const createError = createMutation.error ? mutationErrorMessage(createMutation.error) : "";

  return (
    <>
      <PageHeader
        title="API keys"
        description="Personal keys authenticate agents and CI jobs as you. Revoking a key takes effect immediately."
      />

      <CreateKeyForm
        onCreate={async (name) => {
          await createMutation.mutateAsync(name).catch(() => {});
        }}
        busy={createMutation.isPending}
        error={createError}
      />

      {oneTimeKey ? (
        <OneTimeKeyNotice
          name={oneTimeKey.name}
          value={oneTimeKey.value}
          onDismiss={() => setOneTimeKey(null)}
        />
      ) : null}

      {notice ? (
        <p className="notice notice-success" role="status" data-testid="keys-notice">
          <Icon name="check" />
          <span className="notice-body">{notice}</span>
        </p>
      ) : null}
      {revokeError ? (
        <p className="notice notice-danger" role="alert" data-testid="revoke-error">
          <Icon name="alert" />
          <span className="notice-body">{revokeError}</span>
        </p>
      ) : null}

      <section className="section">
        <div className="section-head">
          <h2 className="section-title" id="api-keys-title">
            Your keys
          </h2>
          <p className="section-note">
            {activeCount} active of {keys.length} total
          </p>
        </div>

        {keys.length === 0 ? (
          <EmptyState
            icon="key"
            title="You have no API keys yet."
            detail="Create one to authenticate an agent or CI job against MCP Runtime."
            testId="api-keys-empty"
          />
        ) : (
          <ApiKeyTable
            keys={keys}
            revokingId={revokeMutation.isPending ? (revokeMutation.variables ?? "") : ""}
            onRequestRevoke={(key) => {
              setNotice("");
              setRevokeError("");
              revokeMutation.reset();
              setPendingRevoke(key);
            }}
          />
        )}
      </section>

      {pendingRevoke ? (
        <ConfirmDialog
          title={`Revoke ${pendingRevoke.name || "this key"}?`}
          body={
            <>
              Any agent or CI job still using <strong>{pendingRevoke.prefix}…</strong> stops
              authenticating immediately. This cannot be undone; you would need to create a new key.
            </>
          }
          confirmLabel="Revoke key"
          destructive
          busy={revokeMutation.isPending}
          testId="revoke-confirm"
          confirmTestId="revoke-confirm-yes"
          cancelTestId="revoke-confirm-cancel"
          onCancel={() => setPendingRevoke(null)}
          onConfirm={() => {
            void revokeMutation.mutateAsync(pendingRevoke.id).catch(() => {});
          }}
        />
      ) : null}
    </>
  );
}
