export type BadgeTone =
  | "ready"
  | "attention"
  | "neutral"
  | "unknown"
  | "low"
  | "medium"
  | "high"
  | "success"
  | "warning"
  | "danger"
  | "info";

type StatusBadgeProps = {
  tone: BadgeTone;
  children: string;
  // Optional longer name for assistive technology when the visible text is terse.
  label?: string;
  dot?: boolean;
  testId?: string;
};

// Status is never colour-only: every badge carries its own text, and the
// optional dot is decorative.
export function StatusBadge({ tone, children, label, dot = true, testId }: StatusBadgeProps) {
  return (
    <span className={`badge badge-${tone}`} aria-label={label} data-testid={testId}>
      {dot ? <span className="dot" aria-hidden="true" /> : null}
      {children}
    </span>
  );
}

export function riskTone(risk: string | undefined): BadgeTone {
  switch ((risk || "").toLowerCase()) {
    case "high":
      return "high";
    case "medium":
      return "medium";
    case "low":
      return "low";
    default:
      return "unknown";
  }
}

// Drift has exactly three server-side values (services/runtime-api tools.go):
// declared, missing, ungoverned.
export function driftTone(drift: string | undefined): BadgeTone {
  switch ((drift || "").toLowerCase()) {
    case "declared":
      return "ready";
    case "missing":
      return "attention";
    case "ungoverned":
      return "warning";
    default:
      return "unknown";
  }
}

export function decisionTone(decision: string | undefined): BadgeTone {
  switch ((decision || "").toLowerCase()) {
    case "allow":
    case "allowed":
      return "ready";
    case "deny":
    case "denied":
      return "attention";
    default:
      return "unknown";
  }
}

export function outcomeTone(status: string | undefined): BadgeTone {
  switch ((status || "").toLowerCase()) {
    case "success":
    case "ok":
      return "ready";
    case "error":
    case "failed":
    case "failure":
    case "denied":
      return "attention";
    case "":
      return "unknown";
    default:
      return "neutral";
  }
}
