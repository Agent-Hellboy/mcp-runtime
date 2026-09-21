import { useState, type FormEvent } from "react";

import { Button } from "../../ui/Button";
import { TextField } from "../../ui/Field";

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
    <form className="form-panel" onSubmit={handleSubmit} data-testid="create-key-form" noValidate>
      <h2 className="form-panel-title">Create an API key</h2>
      <div className="filter-bar" style={{ marginBottom: 0 }}>
        <TextField
          label="Key name"
          fieldClassName="grow"
          value={name}
          maxLength={MAX_NAME_LENGTH}
          autoComplete="off"
          placeholder="Laptop, CI runner, …"
          hint="Shown in this list and in the audit trail. The key value itself is displayed once, right after creation."
          error={shown || undefined}
          errorTestId="create-key-error"
          announceError
          data-testid="create-key-name"
          onChange={(event) => {
            setName(event.target.value);
            if (validation) {
              setValidation("");
            }
          }}
        />
        <Button type="submit" variant="primary" icon="plus" busy={busy} data-testid="create-key-submit">
          {busy ? "Creating…" : "Create key"}
        </Button>
      </div>
    </form>
  );
}
