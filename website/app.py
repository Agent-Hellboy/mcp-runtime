from flask import Flask, abort, redirect, render_template, send_from_directory
from werkzeug.exceptions import NotFound

app = Flask(__name__)

NAV_LINKS = [
    {"label": "Product", "href": "#product"},
    {"label": "Workflow", "href": "#workflow"},
    {"label": "CLI", "href": "#cli"},
    {"label": "Sentinel", "href": "#sentinel"},
    {"label": "Docs", "href": "#docs"},
    {"label": "Contact", "href": "#contact"},
]

HERO = {
    "badge": "mcpruntime.org",
    "title": "Run internal MCP servers like a platform.",
    "subtitle": (
        "MCP Runtime is a self-hosted Kubernetes control plane for shipping, "
        "governing, and observing MCP servers. It combines CRDs, operator "
        "reconciliation, registry and ingress workflows, per-tool access "
        "controls, and the bundled mcp-sentinel analytics plane."
    ),
    "primary": {"label": "Read the docs", "href": "/docs/"},
    "secondary": {"label": "Open API reference", "href": "/docs/api"},
}

AT_A_GLANCE = [
    "The CLI already covers setup, cluster, status, server, registry, and pipeline flows.",
    "Three core resources separate deployment, access grants, and consented agent sessions.",
    "Gateway mode enforces per-tool policy, trust ceilings, and allow or deny audit decisions.",
    "mcp-sentinel adds ingest, processor, API, UI, gateway, storage, and observability services.",
]

STATS = [
    {
        "value": "6 CLI groups",
        "label": "setup, cluster, status, server, registry, and pipeline are already wired in the repo.",
    },
    {
        "value": "3 core resources",
        "label": "MCPServer, MCPAccessGrant, and MCPAgentSession model deploy, access, and consent.",
    },
    {
        "value": "14 bundled workloads",
        "label": "mcp-runtime status checks ClickHouse, Kafka, ingest, API, UI, gateway, and the observability stack.",
    },
    {
        "value": "README essentials in docs",
        "label": "Requirements, quick start, key commands, architecture, and current scope now live in the docs area.",
    },
]

PLATFORM = {
    "title": "What ships in the repo today",
    "intro": (
        "This is no longer just an operator skeleton. The runtime already spans "
        "bootstrap, delivery, access control, gateway enforcement, and analytics."
    ),
    "items": [
        {
            "tag": "Bootstrap",
            "title": "Cluster, ingress, registry, and TLS setup",
            "body": (
                "mcp-runtime setup can initialize namespaces and CRDs, install "
                "ingress, deploy the internal registry or use a provisioned one, "
                "and layer in cert-manager based TLS when needed."
            ),
            "highlights": [
                "Kind, EKS, GKE, and AKS aware cluster flows",
                "Internal registry or external registry configuration",
                "with-tls and without-sentinel setup switches",
            ],
        },
        {
            "tag": "Runtime",
            "title": "Operator-managed MCP server delivery",
            "body": (
                "MCPServer drives image, replicas, ports, ingress, resources, env "
                "vars, secret-backed env vars, tool inventory, auth, policy, "
                "session, gateway, analytics, and rollout strategy for each server."
            ),
            "highlights": [
                "Deployment, Service, and Ingress reconciliation",
                "Route defaults to /{server-name}/mcp",
                "RollingUpdate, Recreate, and Canary rollouts",
            ],
        },
        {
            "tag": "Access",
            "title": "Separate grants and sessions for authorization state",
            "body": (
                "MCPAccessGrant and MCPAgentSession move access policy out of the "
                "deployment object so human and agent subjects, trust ceilings, "
                "consent, expiry, and revocation stay first-class."
            ),
            "highlights": [
                "Per-tool allow or deny rules",
                "low, medium, and high trust levels",
                "Revocation and upstream token references",
            ],
        },
        {
            "tag": "Delivery",
            "title": "Build, push, generate, and deploy from one CLI",
            "body": (
                "The repo includes image build helpers, direct or in-cluster "
                "registry push flows, metadata-to-CRD generation, and manifest "
                "deployment so local and CI/CD paths use the same surface."
            ),
            "highlights": [
                "server build image for image creation",
                "registry push in direct or in-cluster mode",
                "pipeline generate and pipeline deploy for metadata workflows",
            ],
        },
    ],
}

WORKFLOW = [
    {
        "step": "01",
        "title": "Provision or connect a cluster",
        "body": (
            "Use cluster provision for a new Kind or cloud path, or point the CLI "
            "at an existing kubeconfig and context."
        ),
    },
    {
        "step": "02",
        "title": "Bootstrap the platform",
        "body": (
            "Run setup to install CRDs, namespaces, ingress, registry, operator, "
            "gateway image wiring, and the bundled mcp-sentinel stack."
        ),
    },
    {
        "step": "03",
        "title": "Describe servers and trust requirements",
        "body": (
            "Define metadata or CRDs for tools, auth headers, policy mode, "
            "session requirements, analytics emission, and rollout strategy."
        ),
    },
    {
        "step": "04",
        "title": "Publish and deploy",
        "body": (
            "Build images, push them to the chosen registry, generate manifests "
            "from metadata when needed, and apply them to the cluster."
        ),
    },
    {
        "step": "05",
        "title": "Grant access and observe behavior",
        "body": (
            "Create access grants and agent sessions, then use status commands and "
            "mcp-sentinel UI or APIs to inspect runtime and audit activity."
        ),
    },
]

CLI_SURFACE = {
    "title": "CLI surface",
    "intro": (
        "The CLI is the product front door today. These command groups already "
        "exist in the codebase and are the path users actually operate."
    ),
    "items": [
        {
            "command": "mcp-runtime setup",
            "title": "Bootstrap the platform stack",
            "body": (
                "Installs CRDs, namespaces, ingress, registry, operator, gateway "
                "proxy image, and the bundled sentinel stack. Flags cover TLS, "
                "test mode, registry sizing, and skipping sentinel."
            ),
            "highlights": [
                "--with-tls",
                "--without-sentinel",
                "--registry-type and --registry-storage",
            ],
        },
        {
            "command": "mcp-runtime cluster",
            "title": "Manage cluster lifecycle and ingress",
            "body": (
                "Includes init, status, config, provision, and cert commands. "
                "The config path handles kubeconfig, ingress manifests, and cloud "
                "provider credential wiring."
            ),
            "highlights": [
                "cluster init and cluster status",
                "cluster config for ingress and kubeconfig",
                "cluster provision plus cert status, apply, and wait",
            ],
        },
        {
            "command": "mcp-runtime server",
            "title": "Work with MCPServer resources and image builds",
            "body": (
                "List, get, create, delete, inspect logs, and inspect runtime "
                "status for MCP servers. The build helper nests under server build "
                "and focuses on image creation."
            ),
            "highlights": [
                "server list, get, create, delete, logs, and status",
                "server build image",
                "server create from flags or a YAML file",
            ],
        },
        {
            "command": "mcp-runtime registry",
            "title": "Push to the internal or external registry",
            "body": (
                "Show registry status and connection info, configure an external "
                "registry, and retag or push images with either direct Docker push "
                "or an in-cluster skopeo helper job."
            ),
            "highlights": [
                "registry status and registry info",
                "registry provision for external registries",
                "registry push --mode in-cluster or direct",
            ],
        },
        {
            "command": "mcp-runtime pipeline",
            "title": "Generate CRDs from metadata and deploy them",
            "body": (
                "Pipeline commands turn .mcp metadata into Kubernetes manifests and "
                "apply those manifests to a target namespace for CI/CD friendly "
                "delivery."
            ),
            "highlights": [
                "pipeline generate --dir .mcp --output manifests",
                "pipeline deploy --dir manifests",
                "Namespace override support on deploy",
            ],
        },
        {
            "command": "mcp-runtime status",
            "title": "Inspect the entire platform in one view",
            "body": (
                "The top-level status command checks the cluster, registry, "
                "operator, MCP servers, and the sentinel workloads so teams can "
                "spot missing or pending components quickly."
            ),
            "highlights": [
                "Registry and operator readiness",
                "All bundled sentinel workloads",
                "Quick MCPServer inventory",
            ],
        },
    ],
}

SENTINEL = {
    "title": "mcp-sentinel analytics and gateway plane",
    "intro": (
        "The companion repo is broader than a dashboard. It holds the analytics, "
        "gateway, and policy-enforcement services that sit around MCP servers."
    ),
    "items": [
        {
            "tag": "Gateway",
            "title": "Transparent MCP proxy sidecar",
            "body": (
                "The proxy can sit in front of a server, read human, agent, and "
                "session headers, enforce tool-level policy, and emit allow or "
                "deny events with trust context."
            ),
            "highlights": [
                "Default identity headers for human, agent, and session IDs",
                "allow-list and observe policy modes",
                "Per-tool trust-aware allow or deny decisions",
            ],
        },
        {
            "tag": "Ingest",
            "title": "Event ingestion service",
            "body": (
                "POST /events writes audit and traffic data into Kafka. API keys "
                "or optional OIDC JWT validation can protect the ingest path."
            ),
            "highlights": [
                "Kafka topic defaults to mcp.events",
                "Health and metrics endpoints included",
                "API key and optional bearer-token auth",
            ],
        },
        {
            "tag": "Processing",
            "title": "Kafka to ClickHouse pipeline",
            "body": (
                "The processor consumes Kafka batches and writes structured event "
                "records into ClickHouse, including indexed fields for server, "
                "namespace, decision, and tool name."
            ),
            "highlights": [
                "ClickHouse backed event table",
                "Batch size and flush interval controls",
                "Indexed audit dimensions for filtering",
            ],
        },
        {
            "tag": "Query",
            "title": "API and UI for audit exploration",
            "body": (
                "The API exposes events, stats, sources, event types, and filtered "
                "queries. The UI surfaces totals, latest source, last event, auto "
                "refresh, and the live event stream."
            ),
            "highlights": [
                "GET /api/events and GET /api/stats",
                "Filter by server, agent, session, decision, and tool",
                "Simple built-in dashboard UI",
            ],
        },
        {
            "tag": "Observability",
            "title": "Full metrics, traces, and logs path",
            "body": (
                "The bundled manifests include Prometheus, Grafana, OpenTelemetry "
                "Collector, Tempo, Loki, and Promtail so sentinel emits more than "
                "just audit rows."
            ),
            "highlights": [
                "Prometheus and Grafana dashboards",
                "OTLP export support",
                "Tempo, Loki, and Promtail included",
            ],
        },
        {
            "tag": "Manifests",
            "title": "Deployment-ready stack files and examples",
            "body": (
                "The k8s directory includes namespace, config, secrets, ClickHouse, "
                "Kafka, API, UI, gateway, observability manifests, plus an example "
                "MCP server and sidecar wiring."
            ),
            "highlights": [
                "mcp-sentinel-gateway routes API, ingest, and UI",
                "Example in-cluster MCP server manifest",
                "Standalone sidecar manifest for MCP proxy integration",
            ],
        },
    ],
}

DOCS_LIBRARY = {
    "title": "Documentation that maps to the actual codebase",
    "intro": (
        "The docs area now carries the README essentials plus deeper runtime, CLI, "
        "sentinel, and API pages so the public site covers the full alpha surface."
    ),
    "items": [
        {
            "tag": "Overview",
            "title": "Docs home",
            "body": (
                "Start with requirements, quick start commands, key commands, "
                "current scope, and links to the deeper guides."
            ),
            "href": "/docs/",
            "label": "Open docs",
        },
        {
            "tag": "Runtime",
            "title": "Control plane and architecture",
            "body": (
                "See how MCPServer, MCPAccessGrant, and MCPAgentSession fit with "
                "operator reconciliation, ingress, registry, rollout, and traffic."
            ),
            "href": "/docs/runtime",
            "label": "Read runtime docs",
        },
        {
            "tag": "CLI",
            "title": "Command groups and operational flows",
            "body": (
                "Use concrete command examples for setup, cluster, server, registry, "
                "pipeline, and status workflows."
            ),
            "href": "/docs/cli",
            "label": "Read CLI docs",
        },
        {
            "tag": "Sentinel",
            "title": "Analytics, gateway, and observability",
            "body": (
                "Understand the ingest, processor, API, UI, proxy, data, and "
                "observability pieces that mcp-runtime can deploy by default."
            ),
            "href": "/docs/sentinel",
            "label": "Read sentinel docs",
        },
        {
            "tag": "API",
            "title": "Resource fields and endpoint reference",
            "body": (
                "Use YAML examples, trust semantics, gateway headers, and the "
                "sentinel API endpoint list as the current contract."
            ),
            "href": "/docs/api",
            "label": "Read API docs",
        },
    ],
}

CALLOUT = {
    "title": "Still alpha, but already broad enough to evaluate seriously.",
    "body": (
        "The current repo covers platform bootstrap, MCP server deployment, access "
        "grants, consented sessions, gateway policy, audit events, and the bundled "
        "mcp-sentinel stack. The docs now point at that full surface."
    ),
    "cta": {"label": "Start with the docs", "href": "/docs/"},
}

CONTACTS = [
    {
        "title": "Email",
        "body": "Reach out directly if you want to discuss the project, collaborate, or compare MCP platform ideas.",
        "label": "princekrroshan01@gmail.com",
        "href": "mailto:princekrroshan01@gmail.com",
    },
    {
        "title": "Book a meeting",
        "body": "Use the calendar link if you want to discuss the project, my work, or anything in general.",
        "label": "Book a meeting",
        "href": "https://cal.com/prince-roshan-izyp81",
        "new_tab": True,
    },
    {
        "title": "LinkedIn",
        "body": "Connect on LinkedIn for updates, questions, introductions, or follow-up conversations.",
        "label": "Prince Roshan",
        "href": "https://www.linkedin.com/in/prince-roshan-91131116b/",
        "new_tab": True,
    },
]


@app.route("/")
def home():
    return render_template(
        "index.html",
        nav_links=NAV_LINKS,
        hero=HERO,
        at_a_glance=AT_A_GLANCE,
        stats=STATS,
        platform=PLATFORM,
        workflow=WORKFLOW,
        cli_surface=CLI_SURFACE,
        sentinel=SENTINEL,
        docs_library=DOCS_LIBRARY,
        callout=CALLOUT,
        contacts=CONTACTS,
    )


@app.route("/docs")
def docs_redirect():
    return redirect("/docs/")


@app.route("/docs/")
@app.route("/docs/welcome")
@app.route("/docs/welcome/")
def docs_index():
    return send_from_directory("docs", "index.html")


@app.route("/docs/<path:page>")
def docs_page(page: str):
    page = page.rstrip("/")
    if page in {"", "welcome", "index"}:
        return send_from_directory("docs", "index.html")
    if not page.endswith(".html"):
        page = f"{page}.html"
    try:
        return send_from_directory("docs", page)
    except NotFound:
        abort(404)


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
