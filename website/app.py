from flask import Flask, redirect, render_template, send_from_directory

app = Flask(__name__)

NAV_LINKS = [
    {"label": "Overview", "href": "#overview"},
    {"label": "Features", "href": "#features"},
    {"label": "Workflow", "href": "#workflow"},
    {"label": "Architecture", "href": "#architecture"},
    {"label": "Docs", "href": "/docs/"},
]

HERO = {
    "badge": "docs.mcp-runtime.org",
    "title": "Launch MCP servers with a platform, not a patchwork.",
    "subtitle": (
        "MCP Runtime Platform gives teams a registry, Kubernetes operator, and "
        "CLI that standardizes how MCP servers are defined, built, and routed."
    ),
    "primary": {"label": "Open Documentation", "href": "/docs/"},
    "secondary": {"label": "View features", "href": "#features"},
}

AT_A_GLANCE = [
    "Metadata-driven server definitions in YAML.",
    "Operator creates Deployment, Service, and Ingress automatically.",
    "Internal registry keeps images and metadata in sync.",
    "Unified routes for every server at /{server-name}/mcp.",
]

STATS = [
    {"value": "1 CLI", "label": "Unified workflow for platform and servers."},
    {"value": "3 core services", "label": "Registry, operator, and cluster helpers."},
    {"value": "0 bespoke YAML", "label": "No hand-written manifests for each server."},
]

OVERVIEW = {
    "title": "Why MCP Runtime Platform",
    "intro": (
        "Move from experiments to production with a clear, repeatable approach "
        "to deploying and cataloging MCP servers."
    ),
    "items": [
        {
            "title": "Built for scale",
            "body": (
                "Manage a fleet of MCP servers without third-party gateways or "
                "bespoke routing layers."
            ),
        },
        {
            "title": "Designed for teams",
            "body": (
                "A centralized registry and consistent deployment flow make it "
                "easy to discover and reuse servers."
            ),
        },
        {
            "title": "Opinionated safety",
            "body": "Promote best practices with standardized metadata and CI-ready pipelines.",
        },
    ],
}

FEATURES = [
    {
        "title": "Complete platform",
        "body": "Deploy registry, operator, and helpers in one setup flow built for MCP fleets.",
    },
    {
        "title": "CLI tooling",
        "body": "Manage platform setup, registry operations, and server pipelines from one CLI.",
    },
    {
        "title": "Kubernetes operator",
        "body": "Creates Deployment, Service, and Ingress resources from MCP metadata.",
    },
    {
        "title": "Metadata-driven",
        "body": "Define MCP servers in YAML without hand-writing Kubernetes manifests.",
    },
    {
        "title": "Unified routing",
        "body": "Every MCP server is reachable at a consistent /{server-name}/mcp path.",
    },
    {
        "title": "Automated builds",
        "body": "Build Docker images from metadata and keep registry entries up to date.",
    },
]

WORKFLOW = [
    {
        "step": "01",
        "title": "Define",
        "body": "Capture server metadata in YAML, including name, route, and container port.",
    },
    {
        "step": "02",
        "title": "Build",
        "body": "Build Docker images locally or in CI/CD and push them to the registry.",
    },
    {
        "step": "03",
        "title": "Deploy",
        "body": "Generate manifests and deploy MCP servers through the operator.",
    },
    {
        "step": "04",
        "title": "Access",
        "body": "Reach every MCP server through consistent URLs managed by ingress.",
    },
]

ARCHITECTURE = [
    {
        "title": "Developer workstation",
        "body": "Define servers, build images, and push updates from local development.",
    },
    {
        "title": "CI/CD pipeline",
        "body": "Build images, publish to the registry, and generate CRDs automatically.",
    },
    {
        "title": "Kubernetes cluster",
        "body": "The operator watches MCPServer resources and provisions services.",
    },
    {
        "title": "Internal registry",
        "body": "Stores MCP server images and keeps metadata in sync for discovery.",
    },
]

CALLOUT = {
    "title": "Active development notice",
    "body": (
        "MCP Runtime Platform is under active development. APIs, commands, and "
        "behavior may change, and production usage is not recommended yet."
    ),
    "cta": {"label": "Read the documentation", "href": "/docs/"},
}


@app.route("/")
def home():
    return render_template(
        "index.html",
        nav_links=NAV_LINKS,
        hero=HERO,
        at_a_glance=AT_A_GLANCE,
        stats=STATS,
        overview=OVERVIEW,
        features=FEATURES,
        workflow=WORKFLOW,
        architecture=ARCHITECTURE,
        callout=CALLOUT,
    )


@app.route("/docs")
def docs_redirect():
    return redirect("/docs/")


@app.route("/docs/")
def docs_index():
    return send_from_directory("docs", "index.html")


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
