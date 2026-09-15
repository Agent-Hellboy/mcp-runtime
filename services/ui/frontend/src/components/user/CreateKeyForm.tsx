import { useId, useState, type FormEvent } from "react";

type CreateKeyFormProps = {
  onCreate: (name: string) => Promise<void>;
  busy: boolean;
  error: string;
};

const MAX_NAME_LENGTH = 64;

// validateKeyName mirrors the server's own rule (a non-empty name) and adds a
// length bound so the field cannot be used to push an unreasonable payload.
// This is UX; services/runtime-api revalidates.
export function validateKeyName(raw: string): string {
  const name = raw.trim();
  if (!name) {
    return "Enter a name so you can recognise this key later.";
  }
  if (name.length > MAX_NAME_LENGTH) {
    return `Use ${MAX_NAME_LENGTH} characters or fewer.`;
  }
  return "";
}

export function CreateKeyForm({ onCreate, busy, error }: CreateKeyFormProps) {
  const [name, setName] = useState("");
  const [validation, setValidation] = useState("");
  const nameId = useId();
  const errorId = useId();

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) {
      return;
    }
    const message = validateKeyName(name);
    setValidation(message);
    if (message) {
      return;
    }
    await onCreate(name.trim());
    setName("");
  }

  const shown = validation || error;

  return (
    <form className="create-key-form" onSubmit={handleSubmit} data-testid="create-key-form">
      <div className="field grow">
        <label htmlFor={nameId}>Key name</label>
        <input
          id={nameId}
          type="text"
          value={name}
          maxLength={MAX_NAME_LENGTH}
          autoComplete="off"
          placeholder="Laptop, CI runner, …"
          aria-invalid={shown ? true : undefined}
          aria-describedby={shown ? errorId : undefined}
          onChange={(event) => {
            setName(event.target.value);
            if (validation) {
              setValidation("");
            }
          }}
          data-testid="create-key-name"
        />
      </div>
      <button type="submit" className="button primary" disabled={busy} data-testid="create-key-submit">
        {busy ? "Creating…" : "Create key"}
      </button>
      {shown ? (
        <p className="form-error" id={errorId} role="alert" data-testid="create-key-error">
          {shown}
        </p>
      ) : null}
    </form>
  );
}
