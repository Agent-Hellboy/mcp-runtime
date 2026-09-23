import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { listServers } from "../api/catalog";
import type { AuthStatus } from "../api/types";
import { CATALOG_QUERY_KEY } from "../hooks/useCatalog";
import { Button } from "../ui/Button";
import { Icon } from "../ui/Icon";

type AccountMenuProps = {
  auth: AuthStatus;
  authBusy: boolean;
  workspace: string;
  onSignIn: () => void;
  onSignOut: () => void;
  onOpenCatalog: () => void;
};

function accountIdentity(auth: AuthStatus): string {
  const email = auth.principal?.email?.trim();
  if (email) return email;
  const subject = auth.principal?.subject?.trim();
  if (subject) return subject;
  return auth.principal?.auth_type === "ui_api_key" ? "Platform API key" : "Signed-in account";
}

function accountRole(auth: AuthStatus): string {
  const role = auth.principal?.role?.trim().toLowerCase();
  if (role === "admin") return "Administrator";
  if (role === "user") return "User";
  if (!role) return "Authenticated";
  return role.replace(/[_-]+/g, " ").replace(/^\w/, (letter) => letter.toUpperCase());
}

function authMethod(auth: AuthStatus): string | undefined {
  const method = auth.principal?.auth_type?.trim();
  if (!method) return undefined;
  if (method === "ui_api_key") return "API key";
  return method.replace(/[_-]+/g, " ").replace(/^\w/, (letter) => letter.toUpperCase());
}

export function AccountMenu({
  auth,
  authBusy,
  workspace,
  onSignIn,
  onSignOut,
  onOpenCatalog,
}: AccountMenuProps) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const authenticated = auth.authenticated === true;
  const identity = accountIdentity(auth);
  const role = accountRole(auth);
  const method = authMethod(auth);
  const serversQuery = useQuery({
    queryKey: [CATALOG_QUERY_KEY, "servers", ""],
    queryFn: () => listServers(),
    enabled: open && authenticated,
    staleTime: 30_000,
    retry: false,
  });
  const servers = serversQuery.data?.servers ?? [];
  const shownServers = servers.slice(0, 6);

  useEffect(() => {
    setOpen(false);
  }, [workspace, authenticated]);

  useEffect(() => {
    if (!open) return;
    function onPointerDown(event: PointerEvent) {
      if (event.target instanceof Node && !rootRef.current?.contains(event.target)) {
        setOpen(false);
      }
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setOpen(false);
        triggerRef.current?.focus();
      }
    }
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  function openCatalog() {
    setOpen(false);
    onOpenCatalog();
  }

  return (
    <div className="account-menu" ref={rootRef} data-testid="account-menu">
      <button
        ref={triggerRef}
        type="button"
        className="account-trigger"
        aria-label={open ? "Close account menu" : "Open account menu"}
        aria-expanded={open}
        aria-controls="account-menu-panel"
        data-testid="account-trigger"
        onClick={() => setOpen((current) => !current)}
      >
        <span className="account-avatar" aria-hidden="true">
          <Icon name="user" size={16} />
        </span>
        <span className="account-trigger-label">{authenticated ? identity : "Account"}</span>
        <Icon className="account-trigger-chevron" name="chevronDown" size={14} />
      </button>

      {open ? (
        <section className="account-popover" id="account-menu-panel" aria-label="Account details">
          {authenticated ? (
            <>
              <div className="account-popover-heading">
                <span className="account-popover-eyebrow">Signed in as</span>
                <strong className="account-identity" title={identity}>{identity}</strong>
                <div className="account-badges">
                  <span className={role === "Administrator" ? "account-role-badge is-admin" : "account-role-badge"}>
                    {role}
                  </span>
                  {method ? <span className="account-method">{method}</span> : null}
                </div>
                <p className="account-access-scope">
                  {role === "Administrator" ? "Access across all platform namespaces." : "Access scoped to your namespaces."}
                </p>
              </div>

              <div className="account-server-section">
                <div className="account-server-heading">
                  <div>
                    <h2>Deployed MCP servers</h2>
                    <p>Visible to your account</p>
                  </div>
                  {!serversQuery.isPending && !serversQuery.isError ? (
                    <span className="account-server-count">{servers.length}</span>
                  ) : null}
                </div>

                {serversQuery.isPending ? (
                  <p className="account-server-state" role="status">Loading your servers…</p>
                ) : serversQuery.isError ? (
                  <div className="account-server-error" role="alert">
                    <span>Couldn’t load the server list.</span>
                    <button type="button" onClick={() => void serversQuery.refetch()}>Retry</button>
                  </div>
                ) : servers.length === 0 ? (
                  <p className="account-server-state">No MCP servers are visible to your account yet.</p>
                ) : (
                  <ul className="account-server-list">
                    {shownServers.map((server) => (
                      <li className="account-server-item" key={`${server.namespace}/${server.name}`}>
                        <span className="account-server-icon" aria-hidden="true"><Icon name="server" size={14} /></span>
                        <span className="account-server-copy">
                          <strong title={server.name}>{server.name}</strong>
                          <span title={server.namespace}>{server.namespace}</span>
                        </span>
                        <span className="account-server-status">{server.status || "Unknown"}</span>
                      </li>
                    ))}
                  </ul>
                )}
                {servers.length > shownServers.length ? (
                  <p className="account-server-more">and {servers.length - shownServers.length} more</p>
                ) : null}
                <Button variant="secondary" size="sm" icon="server" block onClick={openCatalog}>
                  Open server catalog
                </Button>
              </div>

              <div className="account-popover-footer">
                <Button
                  variant="ghost"
                  size="sm"
                  icon="logout"
                  block
                  disabled={authBusy}
                  onClick={() => {
                    setOpen(false);
                    onSignOut();
                  }}
                  data-testid="logout-button"
                >
                  {authBusy ? "Signing out…" : "Sign out"}
                </Button>
              </div>
            </>
          ) : (
            <div className="account-signed-out">
              <span className="account-avatar account-avatar-large" aria-hidden="true"><Icon name="user" size={19} /></span>
              <h2>Sign in to your account</h2>
              <p>View your deployed servers, tools, and platform access.</p>
              <Button data-testid="account-menu-signin" variant="primary" icon="login" block onClick={() => { setOpen(false); onSignIn(); }}>
                Sign in
              </Button>
            </div>
          )}
        </section>
      ) : null}
    </div>
  );
}
