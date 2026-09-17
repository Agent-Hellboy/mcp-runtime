import { useState, type FormEvent } from "react";

import { Button } from "../../ui/Button";
import { SelectField, TextField } from "../../ui/Field";
import type { ServerSummary } from "../../api/types";

export type GrantDraft = {
  name: string;
  namespace: string;
  server: string;
  humanID: string;
  agentID: string;
  teamID: string;
  maxTrust: string;
  allowedSideEffects: string[];
};

export type SessionDraft = {
  name: string;
  namespace: string;
  server: string;
  humanID: string;
  agentID: string;
  teamID: string;
  consentedTrust: string;
  expiresAt: string;
};

// The three values api/v1alpha1 accepts for a trust level and a side effect.
const TRUST_OPTIONS = [
  { value: "low", label: "low" },
  { value: "medium", label: "medium" },
  { value: "high", label: "high" },
];
export const SIDE_EFFECTS = ["read", "write", "destructive"];

// Kubernetes object names: RFC 1123 subdomain, which is what the API server
// rejects if we get it wrong. Validating here keeps the error next to the field.
const NAME_PATTERN = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

export function validateName(value: string, what: string): string {
  const name = value.trim();
  if (!name) {
    return `Enter a ${what} name.`;
  }
  if (name.length > 63) {
    return "Use 63 characters or fewer.";
  }
  if (!NAME_PATTERN.test(name)) {
    return "Use lowercase letters, digits, and hyphens, starting and ending with a letter or digit.";
  }
  return "";
}

export function validateSubject(draft: { humanID: string; agentID: string; teamID: string }): string {
  if (!draft.humanID.trim() && !draft.agentID.trim() && !draft.teamID.trim()) {
    return "Identify the subject with at least one of human, agent, or team.";
  }
  return "";
}

type ServerChoiceProps = {
  servers: ServerSummary[];
  value: string;
  namespace: string;
  error?: string;
  onChange: (value: string) => void;
};

// Offers the servers the signed-in admin can already read, and still accepts a
// typed name for a server outside the current catalog read.
function ServerChoice({ servers, value, namespace, error, onChange }: ServerChoiceProps) {
  const inScope = servers.filter((server) => !namespace || server.namespace === namespace);
  const known = inScope.some((server) => server.name === value);

  if (inScope.length === 0) {
    return (
      <TextField
        label="Server"
        value={value}
        required
        error={error}
        announceError
        hint="No servers were returned for this namespace; enter the MCPServer name."
        onChange={(event) => onChange(event.target.value)}
      />
    );
  }

  return (
    <SelectField
      label="Server"
      value={known ? value : ""}
      required
      error={error}
      announceError
      hint="MCPServer objects visible in this namespace."
      options={[
        { value: "", label: "Select a server" },
        ...inScope.map((server) => ({ value: server.name, label: server.name })),
      ]}
      onChange={(event) => onChange(event.target.value)}
    />
  );
}

type GrantFormProps = {
  draft: GrantDraft;
  servers: ServerSummary[];
  namespaces: string[];
  busy: boolean;
  submitError: string;
  onChange: (draft: GrantDraft) => void;
  onCancel: () => void;
  onSubmit: () => void;
};

export function GrantForm({
  draft,
  servers,
  namespaces,
  busy,
  submitError,
  onChange,
  onCancel,
  onSubmit,
}: GrantFormProps) {
  const [errors, setErrors] = useState<Record<string, string>>({});

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) {
      return;
    }
    const next: Record<string, string> = {};
    const nameError = validateName(draft.name, "grant");
    if (nameError) next.name = nameError;
    if (!draft.server.trim()) next.server = "Choose the server this grant applies to.";
    const subjectError = validateSubject(draft);
    if (subjectError) next.subject = subjectError;
    if (draft.allowedSideEffects.length === 0) next.effects = "Allow at least one side effect.";
    setErrors(next);
    if (Object.keys(next).length > 0) {
      return;
    }
    onSubmit();
  }

  return (
    <form className="form-panel" onSubmit={handleSubmit} data-testid="grant-create-form" noValidate>
      <h3 className="form-panel-title">Create an access grant</h3>

      <fieldset className="form-fieldset">
        <legend>Identity</legend>
        <div className="form-grid">
          <TextField
            label="Grant name"
            value={draft.name}
            required
            error={errors.name}
            announceError
            data-testid="grant-name"
            onChange={(event) => onChange({ ...draft, name: event.target.value })}
          />
          <SelectField
            label="Namespace"
            value={draft.namespace}
            hint="The grant is created in this namespace."
            options={
              namespaces.includes(draft.namespace)
                ? namespaces.map((ns) => ({ value: ns, label: ns }))
                : [{ value: draft.namespace, label: draft.namespace }, ...namespaces.map((ns) => ({ value: ns, label: ns }))]
            }
            data-testid="grant-namespace"
            onChange={(event) => onChange({ ...draft, namespace: event.target.value })}
          />
          <ServerChoice
            servers={servers}
            value={draft.server}
            namespace={draft.namespace}
            error={errors.server}
            onChange={(value) => onChange({ ...draft, server: value })}
          />
        </div>
      </fieldset>

      <fieldset className="form-fieldset">
        <legend>Subject</legend>
        <div className="form-grid">
          <TextField
            label="Human ID"
            value={draft.humanID}
            hint="Usually the user's email address."
            data-testid="grant-human"
            onChange={(event) => onChange({ ...draft, humanID: event.target.value })}
          />
          <TextField
            label="Agent ID"
            value={draft.agentID}
            data-testid="grant-agent"
            onChange={(event) => onChange({ ...draft, agentID: event.target.value })}
          />
          <TextField
            label="Team ID"
            value={draft.teamID}
            data-testid="grant-team"
            onChange={(event) => onChange({ ...draft, teamID: event.target.value })}
          />
        </div>
        {errors.subject ? (
          <p className="field-error" role="alert" data-testid="grant-subject-error">
            {errors.subject}
          </p>
        ) : null}
      </fieldset>

      <fieldset className="form-fieldset">
        <legend>Policy ceiling</legend>
        <div className="form-grid">
          <SelectField
            label="Maximum trust"
            value={draft.maxTrust}
            options={TRUST_OPTIONS}
            hint="A call is denied when the tool needs more trust than this."
            data-testid="grant-trust"
            onChange={(event) => onChange({ ...draft, maxTrust: event.target.value })}
          />
          <div className="field">
            <span className="field-label" id="grant-effects-label">
              Allowed side effects
            </span>
            <div className="inline-actions" role="group" aria-labelledby="grant-effects-label">
              {SIDE_EFFECTS.map((effect) => {
                const checked = draft.allowedSideEffects.includes(effect);
                return (
                  <label className="chip" key={effect} style={{ paddingLeft: 8, cursor: "pointer" }}>
                    <input
                      type="checkbox"
                      checked={checked}
                      data-testid={`grant-effect-${effect}`}
                      onChange={() =>
                        onChange({
                          ...draft,
                          allowedSideEffects: checked
                            ? draft.allowedSideEffects.filter((value) => value !== effect)
                            : [...draft.allowedSideEffects, effect],
                        })
                      }
                    />
                    {effect}
                  </label>
                );
              })}
            </div>
            {errors.effects ? (
              <span className="field-error" role="alert">
                {errors.effects}
              </span>
            ) : (
              <span className="field-hint">Submitted as written here; nothing is added silently.</span>
            )}
          </div>
        </div>
      </fieldset>

      {submitError ? (
        <p className="notice notice-danger" role="alert" data-testid="grant-create-error">
          <span className="notice-body">{submitError}</span>
        </p>
      ) : null}

      <div className="form-footer">
        <span className="form-footer-note">
          Policy version and tool rules are left at their server-side defaults.
        </span>
        <Button variant="ghost" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
        <Button type="submit" variant="primary" busy={busy} data-testid="grant-create-submit">
          {busy ? "Creating…" : "Create grant"}
        </Button>
      </div>
    </form>
  );
}

type SessionFormProps = {
  draft: SessionDraft;
  servers: ServerSummary[];
  namespaces: string[];
  busy: boolean;
  submitError: string;
  onChange: (draft: SessionDraft) => void;
  onCancel: () => void;
  onSubmit: () => void;
};

export function SessionForm({
  draft,
  servers,
  namespaces,
  busy,
  submitError,
  onChange,
  onCancel,
  onSubmit,
}: SessionFormProps) {
  const [errors, setErrors] = useState<Record<string, string>>({});

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) {
      return;
    }
    const next: Record<string, string> = {};
    const nameError = validateName(draft.name, "session");
    if (nameError) next.name = nameError;
    if (!draft.server.trim()) next.server = "Choose the server this session applies to.";
    const subjectError = validateSubject(draft);
    if (subjectError) next.subject = subjectError;
    if (draft.expiresAt && Number.isNaN(Date.parse(draft.expiresAt))) {
      next.expiresAt = "Enter a valid date and time.";
    }
    setErrors(next);
    if (Object.keys(next).length > 0) {
      return;
    }
    onSubmit();
  }

  return (
    <form className="form-panel" onSubmit={handleSubmit} data-testid="session-create-form" noValidate>
      <h3 className="form-panel-title">Create an agent session</h3>

      <fieldset className="form-fieldset">
        <legend>Identity</legend>
        <div className="form-grid">
          <TextField
            label="Session name"
            value={draft.name}
            required
            error={errors.name}
            announceError
            data-testid="session-name"
            onChange={(event) => onChange({ ...draft, name: event.target.value })}
          />
          <SelectField
            label="Namespace"
            value={draft.namespace}
            hint="The session is created in this namespace."
            options={
              namespaces.includes(draft.namespace)
                ? namespaces.map((ns) => ({ value: ns, label: ns }))
                : [{ value: draft.namespace, label: draft.namespace }, ...namespaces.map((ns) => ({ value: ns, label: ns }))]
            }
            data-testid="session-namespace"
            onChange={(event) => onChange({ ...draft, namespace: event.target.value })}
          />
          <ServerChoice
            servers={servers}
            value={draft.server}
            namespace={draft.namespace}
            error={errors.server}
            onChange={(value) => onChange({ ...draft, server: value })}
          />
        </div>
      </fieldset>

      <fieldset className="form-fieldset">
        <legend>Subject</legend>
        <div className="form-grid">
          <TextField
            label="Human ID"
            value={draft.humanID}
            data-testid="session-human"
            onChange={(event) => onChange({ ...draft, humanID: event.target.value })}
          />
          <TextField
            label="Agent ID"
            value={draft.agentID}
            data-testid="session-agent"
            onChange={(event) => onChange({ ...draft, agentID: event.target.value })}
          />
          <TextField
            label="Team ID"
            value={draft.teamID}
            data-testid="session-team"
            onChange={(event) => onChange({ ...draft, teamID: event.target.value })}
          />
        </div>
        {errors.subject ? (
          <p className="field-error" role="alert" data-testid="session-subject-error">
            {errors.subject}
          </p>
        ) : null}
      </fieldset>

      <fieldset className="form-fieldset">
        <legend>Consent</legend>
        <div className="form-grid">
          <SelectField
            label="Consented trust"
            value={draft.consentedTrust}
            options={TRUST_OPTIONS}
            hint="The ceiling this session consented to, capped again by the grant."
            data-testid="session-trust"
            onChange={(event) => onChange({ ...draft, consentedTrust: event.target.value })}
          />
          <TextField
            label="Expires at"
            type="datetime-local"
            value={draft.expiresAt}
            error={errors.expiresAt}
            announceError
            hint="Leave empty for no expiry."
            data-testid="session-expires"
            onChange={(event) => onChange({ ...draft, expiresAt: event.target.value })}
          />
        </div>
      </fieldset>

      {submitError ? (
        <p className="notice notice-danger" role="alert" data-testid="session-create-error">
          <span className="notice-body">{submitError}</span>
        </p>
      ) : null}

      <div className="form-footer">
        <span className="form-footer-note">Policy version is left at its server-side default.</span>
        <Button variant="ghost" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
        <Button type="submit" variant="primary" busy={busy} data-testid="session-create-submit">
          {busy ? "Creating…" : "Create session"}
        </Button>
      </div>
    </form>
  );
}
