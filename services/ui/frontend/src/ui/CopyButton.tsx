import { useEffect, useRef, useState } from "react";

import { IconButton } from "./Button";

type CopyButtonProps = {
  value: string;
  label: string;
  size?: "sm" | "md";
  testId?: string;
};

type CopyState = "idle" | "copied" | "failed";

// Clipboard access can be denied, unavailable in an insecure context, or
// rejected by the user. The result is reported honestly either way; the caller
// always keeps the full value visible or selectable next to this control.
export function CopyButton({ value, label, size = "sm", testId }: CopyButtonProps) {
  const [state, setState] = useState<CopyState>("idle");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => () => clearTimeout(timer.current), []);

  async function copy() {
    clearTimeout(timer.current);
    try {
      if (!navigator.clipboard?.writeText) {
        throw new Error("clipboard unavailable");
      }
      await navigator.clipboard.writeText(value);
      setState("copied");
    } catch {
      setState("failed");
    }
    timer.current = setTimeout(() => setState("idle"), 2600);
  }

  return (
    <>
      <IconButton
        icon={state === "copied" ? "check" : "copy"}
        label={label}
        size={size}
        data-testid={testId}
        onClick={() => void copy()}
      />
      <span className={state === "idle" ? "visually-hidden" : `copy-status ${state === "copied" ? "ok" : "fail"}`} role="status">
        {state === "copied" ? "Copied" : state === "failed" ? "Copy failed — select the value manually" : ""}
      </span>
    </>
  );
}

type CopyFieldProps = {
  value: string;
  label: string;
  testId?: string;
};

// A truncated value plus a copy control that always copies the whole string.
export function CopyField({ value, label, testId }: CopyFieldProps) {
  return (
    <span className="copy-row">
      <span className="copy-value" title={value}>
        {value}
      </span>
      <CopyButton value={value} label={label} testId={testId} />
    </span>
  );
}
