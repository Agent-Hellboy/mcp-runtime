import { LegacyDashboard } from "./LegacyDashboard";

export function LegacyWorkspace() {
  return (
    <section className="panel legacy-workspace" aria-labelledby="legacy-workspace-title">
      <div className="panel-head">
        <div>
          <h2 id="legacy-workspace-title">More workspaces</h2>
          <p className="panel-lede">
            Activity, keys, analytics, teams, access control, operations, and platform settings
            still run in the previous dashboard while they are migrated. Role-based visibility is
            unchanged.
          </p>
        </div>
        <a className="button ghost" href="/legacy/index.html" target="_blank" rel="noreferrer">
          Open in a new tab
        </a>
      </div>
      <LegacyDashboard />
    </section>
  );
}
