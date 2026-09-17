import { useCallback, useId, useState, type FormEvent } from "react";

import { GoogleSignInButton } from "./GoogleSignInButton";
import type { LoginInput } from "../api/auth";

type SignInPanelProps = {
  onSubmit: (input: LoginInput) => Promise<void>;
  onCancel?: () => void;
  error: string;
  busy: boolean;
};

export function SignInPanel({ onSubmit, onCancel, error, busy }: SignInPanelProps) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [apiKey, setApiKey] = useState("");
  const emailId = useId();
  const passwordId = useId();
  const apiKeyId = useId();

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) {
      return;
    }
    const key = apiKey.trim();
    await onSubmit(key ? { apiKey: key } : { email: email.trim(), password });
  }

  const handleGoogleCredential = useCallback(
    (idToken: string) => {
      void onSubmit({ idToken });
    },
    [onSubmit]
  );

  return (
    <section className="panel signin-panel" aria-labelledby="signin-title">
      <h2 id="signin-title">Sign in to MCP Sentinel</h2>
      <p className="panel-lede">
        The server catalog and tool inventory are scoped to your account. Sign in with your
        platform account or an MCP Sentinel API key.
      </p>
      <form className="signin-form" onSubmit={handleSubmit} data-testid="login-form">
        <div className="field">
          <label htmlFor={emailId}>Email</label>
          <input
            id={emailId}
            type="email"
            autoComplete="username"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            data-testid="login-email"
          />
        </div>
        <div className="field">
          <label htmlFor={passwordId}>Password</label>
          <input
            id={passwordId}
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            data-testid="login-password"
          />
        </div>
        <div className="field">
          <label htmlFor={apiKeyId}>API key</label>
          <input
            id={apiKeyId}
            type="password"
            autoComplete="off"
            placeholder="Optional alternative to email and password"
            value={apiKey}
            onChange={(event) => setApiKey(event.target.value)}
            data-testid="login-api-key"
          />
        </div>
        {error ? (
          <p className="form-error" role="alert" data-testid="login-error">
            {error}
          </p>
        ) : null}
        <div className="form-actions">
          <button type="submit" className="button primary" disabled={busy} data-testid="login-submit">
            {busy ? "Signing in…" : "Continue"}
          </button>
          {onCancel ? (
            <button type="button" className="button ghost" onClick={onCancel} disabled={busy}>
              Cancel
            </button>
          ) : null}
        </div>
      </form>
      <GoogleSignInButton onCredential={handleGoogleCredential} />
    </section>
  );
}
