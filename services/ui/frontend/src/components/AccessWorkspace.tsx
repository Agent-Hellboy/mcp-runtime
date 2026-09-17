import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { AccessControlPanel, type AccessSelection } from "./admin/AccessControlPanel";
import { AccessDetail } from "./admin/AccessDetail";
import { EmptyState } from "./EmptyState";
import { LoadingState } from "./LoadingState";
import { listNamespaces } from "../api/catalog";
import { isAdmin, type AuthStatus } from "../api/types";
import { CATALOG_QUERY_KEY } from "../hooks/useCatalog";

type AccessWorkspaceProps = {
  auth: AuthStatus;
  onSignIn: () => void;
};

// Grants and agent sessions are any authenticated user's territory, not
// admin-only - the backend registers /runtime/grants and /runtime/sessions
// with its plain auth() middleware, not adminOnly().
export function AccessWorkspace({ auth, onSignIn }: AccessWorkspaceProps) {
  const [namespace, setNamespace] = useState("");
  const [selection, setSelection] = useState<AccessSelection | null>(null);

  // Shares ServersWorkspace's query cache/key, so no extra request when that
  // catalog read has already happened this session.
  const namespacesQuery = useQuery({
    queryKey: [CATALOG_QUERY_KEY, "namespaces"],
    queryFn: listNamespaces,
    enabled: auth.authenticated,
  });

  // The runtime API resolves an empty namespace by role
  // (services/runtime-api/internal/runtimeapi/subject_binding.go,
  // scopedNamespaceForPrincipal): admin gets every namespace cluster-wide,
  // but a non-admin with no team namespace gets a 403 ("forbidden namespace"
  // / principal identity required) rather than a default scope - there is no
  // such thing as a non-admin who legitimately wants an empty namespace, so
  // this re-applies every time namespace goes back to "", not just once.
  // (It previously only applied once via a ref; a non-admin who cleared the
  // "All namespaces"-labelled input back to empty stayed on the broken empty
  // scope forever, 403ing both tables.) Mirrors the legacy dashboard's
  // #scope-namespace default (syncScopeSelector's scopes[0] fallback).
  useEffect(() => {
    if (isAdmin(auth) || namespace !== "") {
      return;
    }
    const first = namespacesQuery.data?.[0]?.namespace;
    if (first) {
      setNamespace(first);
    }
  }, [auth, namespace, namespacesQuery.data]);

  if (!auth.authenticated) {
    return (
      <section className="panel" aria-labelledby="access-signed-out-title">
        <h2 id="access-signed-out-title">Access control</h2>
        <EmptyState
          title="Sign in to view access control."
          detail="Grants and agent sessions enforced by the MCP gateway are visible to any signed-in account."
          testId="access-signed-out"
          action={
            <button type="button" className="button primary" onClick={onSignIn}>
              Sign in
            </button>
          }
        />
      </section>
    );
  }

  if (selection) {
    return <AccessDetail selection={selection} onBack={() => setSelection(null)} />;
  }

  // Admin always queries with the namespace as-is ("" means cluster-wide).
  // A non-admin with an empty namespace must wait for the default above to
  // settle before AccessControlPanel (and its grants/sessions queries) ever
  // mounts - otherwise the first request goes out empty, 403s, and
  // immediately refires once corrected. Give up and proceed once the
  // namespaces read has resolved with nothing to default to, so a principal
  // with zero visible namespaces still reaches a rendered (if empty) state
  // rather than a permanent spinner.
  const hasDefaultToApply =
    namespacesQuery.isSuccess && Boolean(namespacesQuery.data[0]?.namespace);
  const waitingForNamespaceDefault =
    !isAdmin(auth) &&
    namespace === "" &&
    (namespacesQuery.isPending || hasDefaultToApply);
  if (waitingForNamespaceDefault) {
    return <LoadingState label="Loading access control…" testId="access-loading" />;
  }

  return (
    <AccessControlPanel
      namespace={namespace}
      onNamespaceChange={setNamespace}
      onSelect={setSelection}
      onSignIn={onSignIn}
    />
  );
}
