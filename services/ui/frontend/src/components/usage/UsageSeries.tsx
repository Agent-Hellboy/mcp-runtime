import type { UsageTimePoint } from "../../api/types";
import { formatTimestamp } from "../../lib/format";

export function UsageSeries({ points, testId = "usage-series" }: { points: UsageTimePoint[]; testId?: string }) {
  if (points.length === 0) return null;
  const width = 720;
  const height = 168;
  const padX = 16;
  const padY = 18;
  const peak = Math.max(...points.map((point) => point.events), 1);
  const x = (index: number) => padX + (index / Math.max(points.length - 1, 1)) * (width - padX * 2);
  const y = (value: number) => height - padY - (value / peak) * (height - padY * 2);
  const line = points.map((point, index) => `${x(index)},${y(point.events)}`).join(" ");

  return (
    <section className="usage-series" data-testid={testId} aria-labelledby={`${testId}-title`}>
      <div className="section-head">
        <div>
          <h2 className="section-title" id={`${testId}-title`}>Requests over time</h2>
          <p className="section-note">Real analytics buckets returned by the selected window.</p>
        </div>
        <span className="series-legend"><span className="series-dot" aria-hidden="true" /> Requests</span>
      </div>
      <div className="usage-series-scroll" tabIndex={0} aria-label="Requests over time chart">
        <svg className="usage-series-chart" viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`${points.length} time buckets, peak ${peak.toLocaleString()} requests`}>
          <line x1={padX} y1={height - padY} x2={width - padX} y2={height - padY} className="series-axis" />
          <polyline points={line} className="series-line" fill="none" />
          {points.map((point, index) => (
            <circle key={`${point.bucket}-${index}`} cx={x(index)} cy={y(point.events)} r="3" className="series-point">
              <title>{`${formatTimestamp(point.bucket)}: ${point.events.toLocaleString()} requests`}</title>
            </circle>
          ))}
        </svg>
      </div>
      <div className="series-range" aria-hidden="true">
        <span>{formatTimestamp(points[0].bucket)}</span>
        <span>{formatTimestamp(points[points.length - 1].bucket)}</span>
      </div>
    </section>
  );
}
