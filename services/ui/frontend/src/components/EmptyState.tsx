import type { ReactNode } from "react";

type EmptyStateProps = {
  title: string;
  detail?: string;
  action?: ReactNode;
  testId?: string;
};

export function EmptyState({ title, detail, action, testId = "empty-state" }: EmptyStateProps) {
  return (
    <div className="state-block state-empty" data-testid={testId}>
      <p className="state-title">{title}</p>
      {detail ? <p className="state-detail">{detail}</p> : null}
      {action}
    </div>
  );
}
