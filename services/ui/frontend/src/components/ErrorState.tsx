type ErrorStateProps = {
  title: string;
  detail?: string;
  onRetry?: () => void;
  retryLabel?: string;
  testId?: string;
};

export function ErrorState({
  title,
  detail,
  onRetry,
  retryLabel = "Try again",
  testId = "error-state",
}: ErrorStateProps) {
  return (
    <div className="state-block state-error" role="alert" data-testid={testId}>
      <p className="state-title">{title}</p>
      {detail ? <p className="state-detail">{detail}</p> : null}
      {onRetry ? (
        <button type="button" className="button ghost" onClick={onRetry}>
          {retryLabel}
        </button>
      ) : null}
    </div>
  );
}
