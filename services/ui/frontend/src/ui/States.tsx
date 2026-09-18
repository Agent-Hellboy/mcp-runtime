import type { ReactNode } from "react";

import { Icon, type IconName } from "./Icon";

type EmptyStateProps = {
  title: string;
  detail?: string;
  icon?: IconName;
  action?: ReactNode;
  testId?: string;
};

export function EmptyState({ title, detail, icon = "inbox", action, testId = "empty-state" }: EmptyStateProps) {
  return (
    <div className="state" data-testid={testId}>
      <span className="state-icon" aria-hidden="true">
        <Icon name={icon} size={18} />
      </span>
      <p className="state-title">{title}</p>
      {detail ? <p className="state-detail">{detail}</p> : null}
      {action ? <div className="state-actions">{action}</div> : null}
    </div>
  );
}

type ErrorStateProps = {
  title: string;
  detail?: string;
  onRetry?: () => void;
  retryLabel?: string;
  testId?: string;
  children?: ReactNode;
};

export function ErrorState({
  title,
  detail,
  onRetry,
  retryLabel = "Try again",
  testId = "error-state",
  children,
}: ErrorStateProps) {
  return (
    <div className="state state-error" role="alert" data-testid={testId}>
      <span className="state-icon" aria-hidden="true">
        <Icon name="alert" size={18} />
      </span>
      <p className="state-title">{title}</p>
      {detail ? <p className="state-detail">{detail}</p> : null}
      {onRetry || children ? (
        <div className="state-actions">
          {onRetry ? (
            <button type="button" className="btn btn-secondary" onClick={onRetry}>
              {retryLabel}
            </button>
          ) : null}
          {children}
        </div>
      ) : null}
    </div>
  );
}

type LoadingStateProps = {
  label: string;
  testId?: string;
  // Shape of the skeleton shown while the real content loads.
  variant?: "rows" | "cards" | "inline";
  rows?: number;
};

export function LoadingState({ label, testId = "loading-state", variant = "rows", rows = 5 }: LoadingStateProps) {
  if (variant === "inline") {
    return (
      <div className="state state-loading" role="status" data-testid={testId}>
        <Icon name="refresh" className="spin" />
        <span>{label}</span>
      </div>
    );
  }

  return (
    <div role="status" aria-live="polite" data-testid={testId}>
      <span className="visually-hidden">{label}</span>
      {variant === "cards" ? (
        <div className="skeleton-cards" aria-hidden="true">
          {Array.from({ length: rows }).map((_, index) => (
            <div className="skeleton" key={index} style={{ height: 150 }} />
          ))}
        </div>
      ) : (
        <div className="skeleton-rows" aria-hidden="true">
          {Array.from({ length: rows }).map((_, index) => (
            <div className="skeleton" key={index} style={{ height: 18, width: index === 0 ? "38%" : "100%" }} />
          ))}
        </div>
      )}
    </div>
  );
}

export function Skeleton({ height = 16, width = "100%" }: { height?: number; width?: number | string }) {
  return <span className="skeleton" style={{ height, width }} aria-hidden="true" />;
}
