import { Button } from "../../ui/Button";
import { Icon } from "../../ui/Icon";

type SignedOutServersProps = {
  onSignIn: () => void;
};

const POINTS = [
  {
    icon: "server" as const,
    title: "Server discovery",
    body: "Every MCP server deployed into the namespaces your account can read, with its endpoint and rollout state.",
  },
  {
    icon: "shield" as const,
    title: "Tool governance",
    body: "Required trust, declared side effects, risk, and drift between what a server declares and what it serves.",
  },
  {
    icon: "activity" as const,
    title: "Runtime visibility",
    body: "Gateway decisions and usage for the servers and teams you belong to.",
  },
];

// Signed out shows what the console is for and nothing about the catalog
// itself: no server counts, no sample inventory, no implication that private
// data is public.
export function SignedOutServers({ onSignIn }: SignedOutServersProps) {
  return (
    <section className="landing" aria-labelledby="catalog-signed-out-title" data-testid="catalog-signed-out">
      <h1 className="landing-title" id="catalog-signed-out-title">
        The control plane for your MCP servers
      </h1>
      <p className="landing-lede">
        MCP Sentinel shows the servers running on this cluster, the tools they expose, and the policy that
        governs every call. Sign in to see the catalog for your namespaces.
      </p>

      <div className="landing-points">
        {POINTS.map((point) => (
          <div className="landing-point" key={point.title}>
            <h2>
              <Icon name={point.icon} size={15} />
              {point.title}
            </h2>
            <p>{point.body}</p>
          </div>
        ))}
      </div>

      <Button variant="primary" icon="login" onClick={onSignIn}>
        Sign in
      </Button>
    </section>
  );
}
