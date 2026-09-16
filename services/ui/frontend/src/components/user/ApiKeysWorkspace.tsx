import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { ApiKeyTable } from "./ApiKeyTable";
import { CreateKeyForm } from "./CreateKeyForm";
import { OneTimeKeyNotice } from "./OneTimeKeyNotice";
import { EmptyState } from "../EmptyState";
import { ErrorState } from "../ErrorState";
import { LoadingState } from "../LoadingState";
import { CSRFError, UnauthorizedError } from "../../api/client";
import {
  createUserAPIKey,
  listUserAPIKeys,
  revokeUserAPIKey,
} from "../../api/userWorkflows";
import type { AuthStatus } from "../../api/types";
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
  const [pendingRevokeId, setPendingRevokeId] = useState("");
  const [notice, setNotice] = useState("");

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
    onSuccess: () => {
      setPendingRevokeId("");
      setNotice("Key revoked.");
      void queryClient.invalidateQueries({ queryKey: USER_KEYS_QUERY_KEY });
    },
  });

  if (!auth.authenticated) {
    return (
      <section className="panel" aria-labelledby="keys-signed-out-title">
        <h2 id="keys-signed-out-title">API keys</h2>
        <EmptyState
          title="Sign in to manage your API keys."
          detail="Keys are scoped to your platform account."
          testId="keys-signed-out"
          action={
            <button type="button" className="button primary" onClick={onSignIn}>
              Sign in
            </button>
          }
        />
      </section>
    );
  }

  if (!enabled) {
    // A session without a
    // user subject (for example UI API-key login) owns no personal keys.
    return (
      <section className="panel" aria-labelledby="keys-no-identity-title">
        <h2 id="keys-no-identity-title">API keys</h2>
        <EmptyState
          title="This session has no user identity."
          detail="Personal API keys are available when you sign in with a platform account."
          testId="keys-no-identity"
        />
      </section>
    );
  }

  if (keysQuery.isPending) {
    return <LoadingState label="Loading your API keys…" testId="keys-loading" />;
  }

  if (keysQuery.error instanceof UnauthorizedError) {
    return (
      <ErrorState
        title="Your session expired."
        detail="Sign in again to manage your API keys."
        onRetry={onSignIn}
        retryLabel="Sign in"
        testId="keys-unauthorized"
      />
    );
  }

  if (keysQuery.error) {
    return (
      <ErrorState
        title="Your API keys could not be loaded."
        detail={mutationErrorMessage(keysQuery.error)}
        onRetry={() => void keysQuery.refetch()}
        testId="keys-error"
      />
    );
  }

  const createError = createMutation.error ? mutationErrorMessage(createMutation.error) : "";
  const revokeError = revokeMutation.error ? mutationErrorMessage(revokeMutation.error) : "";

  return (
    <div className="user-workspace">
      <section className="panel" aria-labelledby="api-keys-title">
        <div className="panel-head">
          <div>
            <h2 id="api-keys-title">API keys</h2>
            <p className="panel-lede">
              Personal keys authenticate agents and CI jobs as you. Revoking a key takes effect
              immediately.
            </p>
          </div>
        </div>

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

        {notice && !revokeError ? (
          <p className="form-success" role="status" data-testid="keys-notice">
            {notice}
          </p>
        ) : null}
        {revokeError ? (
          <p className="form-error" role="alert" data-testid="revoke-error">
            {revokeError}
          </p>
        ) : null}

        <ApiKeyTable
          keys={keysQuery.data ?? []}
          revokingId={revokeMutation.isPending ? revokeMutation.variables ?? "" : ""}
          pendingRevokeId={pendingRevokeId}
          onRequestRevoke={(id) => {
            setNotice("");
            revokeMutation.reset();
            setPendingRevokeId(id);
          }}
          onConfirmRevoke={(id) => {
            void revokeMutation.mutateAsync(id).catch(() => {});
          }}
          onCancelRevoke={() => setPendingRevokeId("")}
        />
      </section>
    </div>
  );
}
