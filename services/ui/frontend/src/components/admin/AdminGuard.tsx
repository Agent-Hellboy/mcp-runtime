import type { ReactNode } from "react";

import { EmptyState } from "../EmptyState";
import { ErrorState } from "../ErrorState";
import { isAdmin } from "../../api/types";
import type { AuthStatus } from "../../api/types";

type AdminGuardProps = {
  auth: AuthStatus;
  onSignIn: () => void;
  children: ReactNode;
};

// Fail-closed route guard. Admin content renders only when the server-returned
// principal says role === "admin". Every other case - signed out, unknown role,
// tenant user, missing principal - renders a refusal instead of the children,
// so direct navigation cannot reach an admin surface.
export function AdminGuard({ auth, onSignIn, children }: AdminGuardProps) {
  if (!auth.authenticated) {
    return (
      <section className="panel" aria-labelledby="admin-signed-out-title">
        <h2 id="admin-signed-out-title">Administration</h2>
        <EmptyState
          title="Sign in to view administration."
          detail="Governance, operations, and platform health are restricted to platform administrators."
          testId="admin-signed-out"
          action={
            <button type="button" className="button primary" onClick={onSignIn}>
              Sign in
            </button>
          }
        />
      </section>
    );
  }

  if (!isAdmin(auth)) {
    return (
      <section className="panel" aria-labelledby="admin-forbidden-title">
        <h2 id="admin-forbidden-title">Administration</h2>
        <ErrorState
          title="This workspace is restricted to administrators."
          detail={`Your account is signed in as "${auth.principal?.role || "unknown"}". Ask a platform administrator if you need access.`}
          testId="admin-forbidden"
        />
      </section>
    );
  }

  return <>{children}</>;
}
