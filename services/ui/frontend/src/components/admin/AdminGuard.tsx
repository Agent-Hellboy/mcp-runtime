import type { ReactNode } from "react";

import { Button } from "../../ui/Button";
import { PageHeader } from "../../ui/PageHeader";
import { EmptyState, ErrorState } from "../../ui/States";
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
// so direct navigation cannot reach an admin surface. The backend enforces the
// same rule independently; this is not the authorization boundary.
export function AdminGuard({ auth, onSignIn, children }: AdminGuardProps) {
  if (!auth.authenticated) {
    return (
      <>
        <PageHeader title="Administration" />
        <EmptyState
          icon="shield"
          title="Sign in to view administration."
          detail="Governance, operations, and platform health are restricted to platform administrators."
          testId="admin-signed-out"
          action={
            <Button variant="primary" icon="login" onClick={onSignIn}>
              Sign in
            </Button>
          }
        />
      </>
    );
  }

  if (!isAdmin(auth)) {
    return (
      <>
        <PageHeader title="Administration" />
        <ErrorState
          title="This workspace is restricted to administrators."
          detail={`Your account is signed in as "${auth.principal?.role || "unknown"}". Ask a platform administrator if you need access.`}
          testId="admin-forbidden"
        />
      </>
    );
  }

  return <>{children}</>;
}
