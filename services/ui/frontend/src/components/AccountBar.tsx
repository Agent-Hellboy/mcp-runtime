import type { AuthStatus } from "../api/types";

type AccountBarProps = {
  status: AuthStatus;
  busy: boolean;
  onSignIn: () => void;
  onSignOut: () => void;
};

function principalLabel(status: AuthStatus): string {
  const principal = status.principal;
  if (!principal) {
    return "Signed in";
  }
  const role = (principal.role || "user").trim();
  const who = (principal.email || principal.subject || "").trim();
  return who ? `${who} · ${role}` : role;
}

export function AccountBar({ status, busy, onSignIn, onSignOut }: AccountBarProps) {
  if (!status.authenticated) {
    return (
      <div className="account-bar">
        <span className="account-state" data-testid="account-state">
          Signed out
        </span>
        <button
          type="button"
          className="button primary"
          onClick={onSignIn}
          data-testid="signin-button"
        >
          Sign in
        </button>
      </div>
    );
  }

  return (
    <div className="account-bar">
      <span className="account-state" data-testid="account-state">
        {principalLabel(status)}
      </span>
      <button
        type="button"
        className="button ghost"
        onClick={onSignOut}
        disabled={busy}
        data-testid="logout-button"
      >
        {busy ? "Signing out…" : "Sign out"}
      </button>
    </div>
  );
}
