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
    "eyebrow": "Kubernetes-native MCP deployment",
    "title": "Run MCP servers on Kubernetes without hand-written manifests.",
    "subtitle": (
        "MCP Runtime gives platform teams a registry, operator, and CLI that "
        "standardize how MCP servers are defined, built, deployed, and routed "
        "across a cluster."
    ),
    "primary": {"label": "Start with docs", "href": "/docs/"},
    "secondary": {"label": "Open API reference", "href": "/docs/api"},
    "highlights": [
        "Self-hosted control plane",
        "Consistent /{server-name}/mcp routes",
        "No per-server manifests to maintain",
    ],
    "proof_kicker": "What the runtime owns",
    "proof_title": "Describe the server once. Let the platform wire the rest.",
    "proof_intro": (
        "The operator turns one MCP definition into the deployment, service, "
        "and ingress path your team actually uses."
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
            "title": "Definition stays human-sized",
            "body": "Keep routing, image, and MCP-specific settings in one place instead of spreading them across bespoke manifests.",
        },
        {
            "title": "Rollouts stay predictable",
            "body": "Use the same path from local build to cluster deployment for every server your team ships.",
        },
    ],
}

STATUS = {
    "label": "Developer preview",
    "body": (
        "APIs and behavior are still changing. Use MCP Runtime for evaluation, "
        "internal prototypes, and team feedback loops, not production rollout yet."
    ),
    "cta": {"label": "See current docs", "href": "/docs/"},
}

OUTCOMES = [
    {
        "title": "Define once",
        "body": (
            "Describe each MCP server in metadata instead of maintaining a new "
            "bundle of Kubernetes YAML for every service."
        ),
    },
    {
        "title": "Ship with one workflow",
        "body": (
            "Keep platform setup, image publishing, and server deployment under "
            "one CLI-driven path that fits local development and CI."
        ),
    },
    {
        "title": "Route consistently",
        "body": (
            "Give every server the same URL pattern and deployment conventions "
            "so teams spend less time re-learning infrastructure."
        ),
    },
]

WHY = {
    "title": "Why teams reach for it",
    "intro": (
        "The value is a clean operating model for MCP servers that keeps "
        "definitions, build steps, and routing conventions aligned across teams."
    ),
    "items": [
        {
            "title": "Fewer one-off deployment decisions",
            "body": (
                "Stop re-answering the same questions around service names, "
                "ingress paths, and deployment shape every time a new MCP server appears."
            ),
        },
        {
            "title": "Shared language across teams",
            "body": (
                "A single metadata model makes it easier for platform engineers, "
                "application teams, and CI pipelines to talk about the same object."
            ),
        },
        {
            "title": "A better path from experiment to standard",
            "body": (
                "You can start with a small internal server catalog without "
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
            "For groups that need one paved road for internal MCP services "
            "instead of many per-team deployment patterns."
        ),
        "tag": "Standardize rollout",
    },
    {
        "title": "AI infrastructure teams",
        "body": (
            "For teams operating several MCP integrations and wanting consistent "
            "routing, discovery, and deployment shape across them."
        ),
        "tag": "Operate a catalog",
    },
    {
        "title": "Developer platform builders",
        "body": (
            "For organizations that want self-hosted MCP infrastructure to fit "
            "into their existing cluster and CI systems."
        ),
        "tag": "Stay self-hosted",
    },
]

PLATFORM = {
    "title": "One platform path, not a bag of parts",
    "intro": (
        "Registry, operator, and CLI matter because they support one standard "
        "flow from definition to deployment, not because they exist as isolated components."
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
    "title": "Start with the docs, then inspect the API surface",
    "body": (
        "The docs currently focus on the runtime API, gateway model, access "
        "grants, sessions, and analytics-related resources. That is the right "
        "place to understand the current shape of the project."
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
