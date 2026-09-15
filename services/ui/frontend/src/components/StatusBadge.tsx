type Tone = "ready" | "attention" | "neutral" | "low" | "medium" | "high";

type StatusBadgeProps = {
  tone: Tone;
  children: string;
  label?: string;
};

export function StatusBadge({ tone, children, label }: StatusBadgeProps) {
  return (
    <span className={`badge badge-${tone}`} aria-label={label}>
      {children}
    </span>
  );
}

export function riskTone(risk: string | undefined): Tone {
  switch ((risk || "").toLowerCase()) {
    case "high":
      return "high";
    case "medium":
      return "medium";
    case "low":
      return "low";
    default:
      return "neutral";
  }
}
