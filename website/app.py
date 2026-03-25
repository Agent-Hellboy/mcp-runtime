from flask import Flask, abort, redirect, render_template, send_from_directory
from werkzeug.exceptions import NotFound

app = Flask(__name__)

NAV_LINKS = [
    {"label": "Overview", "href": "#overview"},
    {"label": "Features", "href": "#features"},
    {"label": "Workflow", "href": "#workflow"},
    {"label": "Architecture", "href": "#architecture"},
    {"label": "Contact", "href": "#contact"},
    {"label": "Docs", "href": "/docs/"},
]

HERO = {
    "badge": "mcpruntime.org",
    "title": "Self-hosted runtime for internal MCP servers.",
    "subtitle": (
        "Deploy, govern, and observe MCP servers with one CLI, one Kubernetes "
        "control plane, and a bundled path for gateway policy and analytics."
    ),
    "primary": {"label": "Open Documentation", "href": "/docs/"},
    "secondary": {"label": "View API surface", "href": "/docs/api"},
}

AT_A_GLANCE = [
    "CLI groups cover setup, cluster, registry, server, pipeline, and status.",
    "Three core resources model deployment, access, and agent session state.",
    "Gateway mode can enforce per-tool policy and emit audit events.",
    "mcp-sentinel provides ingest, processor, API, UI, and observability services.",
]

STATS = [
    {"value": "3 core resources", "label": "MCPServer, MCPAccessGrant, and MCPAgentSession."},
    {"value": "1 setup path", "label": "CLI-driven install for runtime, ingress, registry, and sentinel."},
    {"value": "Built-in audit flow", "label": "Gateway decisions can flow into the bundled analytics stack."},
]

OVERVIEW = {
    "title": "Core resources",
    "intro": (
        "These resources are the center of the current `v1alpha1` surface."
    ),
    "items": [
        {
            "title": "MCPServer",
            "body": (
                "Defines workload, route, tools, gateway behavior, rollout, and "
                "analytics settings for one MCP server."
            ),
        },
        {
            "title": "MCPAccessGrant",
            "body": (
                "Maps a human or agent to a server and sets max trust and per-tool "
                "allow or deny rules."
            ),
        },
        {
            "title": "MCPAgentSession",
            "body": (
                "Stores consented trust, expiry, revocation, and upstream token "
                "references for active agent sessions."
            ),
        },
    ],
}

FEATURES = [
    {
        "title": "CLI-first setup",
        "body": (
            "`mcp-runtime setup` installs the runtime stack, with flags for TLS, "
            "sentinel, ingress mode, and registry behavior."
        ),
    },
    {
        "title": "Registry workflows",
        "body": (
            "Use the built-in registry or provision an external registry for "
            "runtime and operator images."
        ),
    },
    {
        "title": "Gateway policy",
        "body": (
            "Run allow-list or observe mode, read identity headers, enforce trust, "
            "and write allow or deny events."
        ),
    },
    {
        "title": "Tool inventory and trust",
        "body": (
            "Declare tools on the server, set required trust levels, and combine "
            "that with grants and sessions."
        ),
    },
    {
        "title": "Sentinel analytics",
        "body": (
            "The repo includes ingest, processor, API, UI, gateway, and "
            "observability pieces under `mcp-sentinel`."
        ),
    },
    {
        "title": "TLS and rollout controls",
        "body": (
            "Enable HTTPS overlays and use rolling, recreate, or canary rollout "
            "strategies from the control plane."
        ),
    },
]

WORKFLOW = [
    {
        "step": "01",
        "title": "Bootstrap",
        "body": (
            "Run setup to install registry, operator, ingress, and optionally the "
            "bundled sentinel stack."
        ),
    },
    {
        "step": "02",
        "title": "Describe",
        "body": (
            "Define server metadata, tool inventory, auth headers, gateway, "
            "policy, and analytics settings."
        ),
    },
    {
        "step": "03",
        "title": "Build and push",
        "body": (
            "Build images locally or in CI/CD and push them to the internal or "
            "provisioned registry."
        ),
    },
    {
        "step": "04",
        "title": "Deploy and grant",
        "body": (
            "Apply the CRDs, then create access grants and agent sessions for the "
            "subjects that should reach each server."
        ),
    },
    {
        "step": "05",
        "title": "Observe",
        "body": (
            "Use status commands and mcp-sentinel APIs and UI to inspect runtime "
            "health and allow or deny decisions."
        ),
    },
]

ARCHITECTURE = [
    {
        "title": "CLI and CI entrypoint",
        "body": "The CLI remains the main entrypoint for setup, publish, deploy, and runtime status checks.",
    },
    {
        "title": "Control-plane resources",
        "body": "The current API surface centers on MCPServer, MCPAccessGrant, and MCPAgentSession.",
    },
    {
        "title": "Operator, registry, and ingress",
        "body": "The cluster layer reconciles workloads and keeps consistent routing at /{server-name}/mcp.",
    },
    {
        "title": "Gateway sidecar",
        "body": "Gateway mode adds identity extraction, policy enforcement, trust checks, and audit emission.",
    },
    {
        "title": "mcp-sentinel stack",
        "body": "Ingest, processor, API, UI, and observability services provide the analytics path.",
    },
]

CALLOUT = {
    "title": "Still alpha, but much broader than the original runtime surface.",
    "body": (
        "The project now spans runtime deployment, gateway policy, grants, "
        "sessions, and sentinel analytics. Use the docs and the v1alpha1 types "
        "as the contract while the platform continues to evolve."
    ),
    "cta": {"label": "Read the API reference", "href": "/docs/api"},
}

CONTACTS = [
    {
        "title": "Email",
        "body": "Reach out directly if you want to discuss the project or collaborate.",
        "label": "princekrroshan01@gmail.com",
        "href": "mailto:princekrroshan01@gmail.com",
    },
    {
        "title": "Book a meeting",
        "body": "Use the calendar link if you want to discuss the project or anything in general.",
        "label": "Book a meeting",
        "href": "https://cal.com/prince-roshan-izyp81",
        "new_tab": True,
    },
    {
        "title": "LinkedIn",
        "body": "Connect on LinkedIn for updates, questions, or follow-up conversations.",
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
        overview=OVERVIEW,
        features=FEATURES,
        workflow=WORKFLOW,
        architecture=ARCHITECTURE,
        callout=CALLOUT,
        contacts=CONTACTS,
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
