import { EmptyState } from "../EmptyState";
import { StatusBadge } from "../StatusBadge";
import type { TeamMembership } from "../../api/types";

type TeamMembershipPanelProps = {
  teams: TeamMembership[];
};

export function TeamMembershipPanel({ teams }: TeamMembershipPanelProps) {
  return (
    <section className="panel" aria-labelledby="team-membership-title">
      <div className="panel-head">
        <div>
          <h2 id="team-membership-title">Team membership</h2>
          <p className="panel-lede">
            The teams your account belongs to, and the namespace each one owns.
          </p>
        </div>
      </div>
      {teams.length === 0 ? (
        <EmptyState
          title="You are not a member of any team."
          detail="Team namespaces appear here once an administrator adds you."
          testId="teams-empty"
        />
      ) : (
        <ul className="team-list" data-testid="team-list">
          {teams.map((team) => (
            <li key={team.id || team.slug || team.namespace} data-testid="team-item">
              <div className="team-item-head">
                <h3>{team.name || team.slug || "Team"}</h3>
                {team.role ? <StatusBadge tone="neutral">{team.role}</StatusBadge> : null}
              </div>
              <dl className="team-item-meta">
                <div>
                  <dt>Namespace</dt>
                  <dd>{team.namespace || "—"}</dd>
                </div>
                <div>
                  <dt>Slug</dt>
                  <dd>{team.slug || "—"}</dd>
                </div>
              </dl>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
