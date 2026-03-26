from flask import Flask, abort, redirect, render_template, send_from_directory
from werkzeug.exceptions import NotFound

app = Flask(__name__)

NAV_LINKS = [
    {"label": "Why it works", "href": "#why"},
    {"label": "Workflow", "href": "#workflow"},
    {"label": "Who it's for", "href": "#teams"},
    {"label": "Platform", "href": "#platform"},
    {"label": "Docs", "href": "/docs/"},
]

HERO = {
    "eyebrow": "Platform preview",
    "title": "The control plane for MCP deployments on Kubernetes.",
    "subtitle": (
        "MCP Runtime gives platform teams one path for defining, publishing, "
        "routing, and operating internal MCP services on their own cluster."
    ),
    "primary": {"label": "Start with docs", "href": "/docs/"},
    "secondary": {"label": "Open API reference", "href": "/docs/api"},
    "highlights": [
        "Self-hosted",
        "Registry + operator + CLI",
        "Consistent /{server-name}/mcp routes",
    ],
    "proof_kicker": "Runtime definition",
    "proof_title": "One spec in. A working cluster path out.",
    "proof_intro": (
        "The runtime owns the infrastructure glue so teams can standardize how "
        "MCP services enter the platform."
    ),
    "proof_code": """apiVersion: mcpruntime.org/v1alpha1
kind: MCPServer
metadata:
  name: payments
spec:
  image: registry.example.com/payments-mcp:latest
  port: 8088
  ingressHost: mcp.example.com
  ingressPath: /payments/mcp
  gateway:
    enabled: true""",
    "proof_points": [
        {
            "title": "A single deployment contract",
            "body": "Keep routing, image, and MCP-specific settings in one definition instead of reassembling infrastructure by hand for every service.",
        },
        {
            "title": "Fleet-level conventions",
            "body": "Use one route shape and one deployment model so new MCP services fit the platform without bespoke exceptions.",
        },
    ],
}

STATUS = {
    "label": "Preview release",
    "body": (
        "APIs and behavior are still moving. Use MCP Runtime for evaluation, "
        "internal pilots, and feedback loops, not production rollout yet."
    ),
    "cta": {"label": "See current docs", "href": "/docs/"},
}

OUTCOMES = [
    {
        "title": "One deployment contract",
        "body": (
            "Describe each MCP service in metadata instead of maintaining a new "
            "bundle of Kubernetes YAML for every deployment."
        ),
    },
    {
        "title": "One release path",
        "body": (
            "Keep platform setup, image publishing, and server deployment under "
            "one CLI-driven path that fits local development and CI."
        ),
    },
    {
        "title": "One route shape",
        "body": (
            "Give every server the same URL pattern and deployment conventions "
            "so teams spend less time re-learning infrastructure."
        ),
    },
]

WHY = {
    "title": "Why platform teams adopt it",
    "intro": (
        "The value is not a collection of components. It is a control plane "
        "that keeps definitions, release steps, and runtime conventions aligned."
    ),
    "items": [
        {
            "title": "Less infrastructure drift",
            "body": (
                "Stop re-answering naming, ingress, and deployment questions every "
                "time a new MCP service appears."
            ),
        },
        {
            "title": "Shared contracts across teams",
            "body": (
                "A single metadata model gives platform engineers, product teams, "
                "and CI pipelines the same object to work from."
            ),
        },
        {
            "title": "A cleaner path from pilot to standard",
            "body": (
                "You can start with a small internal service catalog without "
                "locking yourself into ad hoc scripts and hand-maintained manifests."
            ),
        },
    ],
}

WORKFLOW = [
    {
        "step": "01",
        "title": "Define",
        "body": (
            "Write the MCP server spec once with the image, port, and route "
            "shape the operator should own."
        ),
    },
    {
        "step": "02",
        "title": "Build and publish",
        "body": (
            "Use the CLI locally or in CI to build images and keep registry "
            "metadata in sync with what is actually deployable."
        ),
    },
    {
        "step": "03",
        "title": "Deploy and route",
        "body": (
            "Let the operator generate the Deployment, Service, and Ingress "
            "resources needed to expose the server at a stable path."
        ),
    },
    {
        "step": "04",
        "title": "Operate as a fleet",
        "body": (
            "Manage multiple MCP servers with the same conventions rather than "
            "stacking separate gateway or manifest patterns for each one."
        ),
    },
]

TEAMS = [
    {
        "title": "Platform teams",
        "body": (
            "For groups building the paved road for internal MCP services and "
            "trying to avoid a different deployment pattern per team."
        ),
        "tag": "Internal platform",
    },
    {
        "title": "AI infrastructure teams",
        "body": (
            "For teams operating multiple MCP integrations and wanting one "
            "runtime contract across routing, deployment, and discovery."
        ),
        "tag": "AI infra",
    },
    {
        "title": "Developer platform builders",
        "body": (
            "For organizations that want self-hosted MCP infrastructure to fit "
            "into their existing cluster and CI systems."
        ),
        "tag": "Developer experience",
    },
]

PLATFORM = {
    "title": "Built like platform infrastructure",
    "intro": (
        "The CLI, operator, and registry matter because they form one control "
        "plane, not because they appear as three disconnected features."
    ),
}

PLATFORM_COMPONENTS = [
    {
        "title": "CLI",
        "body": (
            "Bootstraps platform setup and drives the same server lifecycle "
            "from local iteration to CI execution."
        ),
    },
    {
        "title": "Operator",
        "body": (
            "Watches MCPServer resources and turns intent into Kubernetes "
            "objects without hand-authoring every manifest."
        ),
    },
    {
        "title": "Registry",
        "body": (
            "Keeps image and metadata records aligned so deployment and "
            "discovery are tied to the same source of truth."
        ),
    },
]

CALLOUT = {
    "title": "Read the docs, then inspect the platform surface",
    "body": (
        "The docs currently focus on the runtime API, gateway model, access "
        "grants, sessions, and analytics-related resources. That is the fastest "
        "way to understand the current shape of the system."
    ),
    "primary": {"label": "Open documentation", "href": "/docs/"},
    "secondary": {"label": "Browse repository", "href": "https://github.com/Agent-Hellboy/mcp-runtime"},
}


@app.route("/")
def home():
    return render_template(
        "index.html",
        nav_links=NAV_LINKS,
        hero=HERO,
        status=STATUS,
        outcomes=OUTCOMES,
        why=WHY,
        workflow=WORKFLOW,
        teams=TEAMS,
        platform=PLATFORM,
        platform_components=PLATFORM_COMPONENTS,
        callout=CALLOUT,
    )


@app.route("/docs")
def docs_redirect():
    return redirect("/docs/")


@app.route("/docs/")
def docs_index():
    return send_from_directory("docs", "index.html")


@app.route("/docs/<path:page>")
def docs_page(page: str):
    if not page.endswith(".html"):
        page = f"{page}.html"
    try:
        return send_from_directory("docs", page)
    except NotFound:
        abort(404)


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
