type LegacyDashboardProps = {
  src?: string;
};

export function LegacyDashboard({ src = "/legacy/index.html" }: LegacyDashboardProps) {
  return (
    <iframe
      className="legacy-dashboard"
      src={src}
      title="MCP Sentinel dashboard"
    />
  );
}
