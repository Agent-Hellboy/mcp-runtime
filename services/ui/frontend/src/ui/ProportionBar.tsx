type ProportionRow = {
  id: string;
  name: string;
  value: number;
  detail?: string;
  tone?: "accent" | "danger";
};

type ProportionListProps = {
  rows: ProportionRow[];
  label: string;
  unit: string;
  testId?: string;
};

// A ranked comparison of aggregate totals. This is deliberately not a time
// series: the usage API returns totals, not timestamped buckets, so there is
// no trend to draw.
export function ProportionList({ rows, label, unit, testId }: ProportionListProps) {
  const max = rows.reduce((peak, row) => Math.max(peak, row.value), 0);

  return (
    <ul className="proportion-list" aria-label={label} data-testid={testId}>
      {rows.map((row) => {
        const share = max > 0 ? Math.round((row.value / max) * 100) : 0;
        return (
          <li className="proportion-row" key={row.id}>
            <span className="proportion-name">{row.name}</span>
            <span className="proportion-value">
              {row.value.toLocaleString()} {unit}
              {row.detail ? ` · ${row.detail}` : ""}
            </span>
            <span
              className="proportion-track"
              role="img"
              aria-label={`${row.name}: ${row.value.toLocaleString()} ${unit}`}
            >
              <span
                className={row.tone === "danger" ? "proportion-fill tone-danger" : "proportion-fill"}
                style={{ width: `${share}%` }}
              />
            </span>
          </li>
        );
      })}
    </ul>
  );
}
