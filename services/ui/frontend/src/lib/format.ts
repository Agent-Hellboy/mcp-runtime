// Shared formatting for timestamps and counts.
//
// Every timestamp the platform returns is an absolute instant. The tables show
// a short local form and keep the full value available on hover and to
// assistive technology, so nobody has to guess what "2d ago" resolves to.

export function formatTimestamp(value: string | undefined): string {
  if (!value) {
    return "—";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString();
}

export function formatAbsolute(value: string | undefined): string {
  if (!value) {
    return "Not recorded";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toISOString();
}

export type ExpiryState = "none" | "valid" | "expired" | "unparseable";

// An expiry we cannot parse is reported as unknown, never as "still valid".
export function expiryState(value: string | undefined, now = Date.now()): ExpiryState {
  const raw = (value || "").trim();
  if (!raw) {
    return "none";
  }
  const parsed = Date.parse(raw);
  if (Number.isNaN(parsed)) {
    return "unparseable";
  }
  return parsed <= now ? "expired" : "valid";
}

export function percent(part: number, total: number): string {
  if (!total) {
    return "—";
  }
  return `${Math.round((part / total) * 100)}%`;
}

// The runtime API reports a server's age as an absolute timestamp. Showing it
// raw makes a card read like a log line, so it becomes a compact duration with
// the exact instant still available through the title attribute.
export function formatAge(value: string | undefined, now = Date.now()): string {
  const raw = (value || "").trim();
  if (!raw) {
    return "";
  }
  const parsed = Date.parse(raw);
  if (Number.isNaN(parsed)) {
    // Already a duration such as "3d" from an older API build.
    return raw;
  }
  const seconds = Math.max(0, Math.round((now - parsed) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.round(minutes / 60);
  if (hours < 48) return `${hours}h`;
  const days = Math.round(hours / 24);
  if (days < 60) return `${days}d`;
  return `${Math.round(days / 30)}mo`;
}
