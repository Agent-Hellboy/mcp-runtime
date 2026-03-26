"""Flask app serving the marketing site and static documentation."""

from flask import Flask, abort, redirect, render_template, send_from_directory
from werkzeug.exceptions import NotFound

app = Flask(__name__)

NAV_LINKS = [
    {"label": "Overview", "href": "#product"},
    {"label": "Documentation", "href": "#docs"},
    {"label": "Workflow", "href": "#workflow"},
    {"label": "Surfaces", "href": "#surfaces"},
]

HERO = {
    "eyebrow": "Open source MCP platform for Kubernetes",
    "title": "Deploy, govern, and observe MCP services on Kubernetes.",
    "subtitle": (
        "MCP Runtime gives platform teams a higher-level control plane for MCP "
        "delivery, access, policy, audit, and observability."
    ),
    "primary": {"label": "Documentation", "href": "/docs/"},
    "secondary": {"label": "Runtime guide", "href": "/docs/runtime"},
    "highlights": [
        "Open source",
        "Kubernetes-native",
        "Governed request path",
    ],
    "proof_kicker": "Reference manifest",
    "proof_title": "One service definition. One operating model.",
    "proof_intro": (
        "The runtime owns rollout and routing from this service definition. "
        "Sentinel governs the live request path with policy, audit, and "
        "observability."
    ),
    "proof_code": """apiVersion: mcpruntime.org/v1alpha1
kind: MCPServer
metadata:
  name: payments
spec:
  image: registry.example.com/payments-mcp:v1.0.0
  port: 8088
  ingressHost: mcp.example.com
  ingressPath: /payments/mcp
  gateway:
    enabled: true
  analytics:
    enabled: true""",
    "stage_items": [
        {
            "label": "Runtime",
            "value": "MCP platform control plane",
            "body": "Bootstrap, reconciliation, rollout, ingress, and lifecycle.",
        },
        {
            "label": "Access",
            "value": "Grants + sessions stay explicit",
            "body": "Consent, trust ceilings, expiry, and revocation stay first-class.",
        },
        {
            "label": "Sentinel",
            "value": "Governed MCP request path",
            "body": "Policy, audit, and observability on live MCP requests.",
        },
    ],
    "stage_signals": [
        "route admitted",
        "policy synced",
        "telemetry flowing",
    ],
}

STATUS = {
    "label": "Alpha",
    "body": (
        "Runtime, access, and the governed request path already work end to end. "
        "The architecture is stable enough to evaluate. The API and UX are still evolving."
    ),
}

PRODUCT = {
    "title": "Three jobs, one platform boundary.",
    "intro": (
        "Keep lifecycle, grants, and request governance distinct without splitting them "
        "across unrelated tools."
    ),
    "items": [
        {
            "label": "Runtime",
            "title": "Control plane for MCP services",
            "body": (
                "Own setup, registry, ingress, reconciliation, and rollout from one "
                "Kubernetes-native surface."
            ),
            "points": [
                "Cluster bootstrap",
                "Reconciliation",
                "Rollout + routes",
            ],
        },
        {
            "label": "Sentinel",
            "title": "Governed request path with policy and observability",
            "body": (
                "Put enforcement, audit, and telemetry on live MCP requests instead "
                "of rebuilding them inside every service."
            ),
            "points": [
                "Proxy enforcement",
                "Audit + telemetry",
                "UI + APIs",
            ],
        },
        {
            "label": "Access",
            "title": "Explicit grants and sessions",
            "body": (
                "Keep entitlement, consent, trust, and revocation in dedicated "
                "resources instead of app-specific conventions."
            ),
            "points": [
                "Entitlement",
                "Consent + expiry",
                "Revocation",
            ],
        },
    ],
}

WORKFLOW = [
    {
        "step": "01",
        "title": "Define once",
        "body": (
            "Describe image, route, gateway, analytics, and access expectations in "
            "one runtime definition."
        ),
    },
    {
        "step": "02",
        "title": "Reconcile",
        "body": (
            "Use the CLI and operator to prepare cluster state and expose the MCP "
            "service through a stable path."
        ),
    },
    {
        "step": "03",
        "title": "Govern live requests",
        "body": (
            "Route requests through the proxy path when gateway mode is enabled so "
            "identity, policy, audit, and telemetry happen in one place."
        ),
    },
    {
        "step": "04",
        "title": "Inspect and iterate",
        "body": (
            "Use grants, sessions, and sentinel surfaces to review behavior and "
            "tighten policy as the service evolves."
        ),
    },
]

SURFACES = {
    "title": "Four surfaces, one documentation model",
    "intro": (
        "Move from architecture to day-two operations without switching mental models."
    ),
    "items": [
        {
            "name": "Runtime",
            "path": "setup / cluster / operator / MCPServer",
            "body": "Prepare clusters and keep MCP services reconciled.",
        },
        {
            "name": "Access",
            "path": "MCPAccessGrant / MCPAgentSession",
            "body": "Keep entitlement, consent, and revocation explicit.",
        },
        {
            "name": "Sentinel",
            "path": "proxy / gateway / ingest / processor / api / ui",
            "body": "Handle live request governance, audit, and observability.",
        },
        {
            "name": "Docs",
            "path": "runtime / cli / sentinel / api",
            "body": "Move from architecture to exact fields and commands quickly.",
        },
    ],
}

DOCS_LIBRARY = {
    "title": "Start with the page that matches the task",
    "intro": (
        "Documentation is the fastest way to understand the platform boundary and the operator workflow."
    ),
    "items": [
        {
            "tag": "Runtime",
            "title": "Control plane and service lifecycle",
            "body": "See cluster bootstrap, reconciliation, rollout, ingress, and delivery state.",
            "href": "/docs/runtime",
            "label": "Runtime docs",
        },
        {
            "tag": "CLI",
            "title": "Commands operators run day to day",
            "body": "Start with setup, cluster, server, registry, pipeline, and status workflows.",
            "href": "/docs/cli",
            "label": "CLI docs",
        },
        {
            "tag": "Sentinel",
            "title": "Governed request path and observability",
            "body": "Review proxy enforcement, audit events, query APIs, and observability.",
            "href": "/docs/sentinel",
            "label": "Sentinel docs",
        },
        {
            "tag": "API",
            "title": "The resource and request contract",
            "body": "Use the API reference for YAML examples, field semantics, headers, and status.",
            "href": "/docs/api",
            "label": "API reference",
        },
    ],
}

CALLOUT = {
    "title": "Start with runtime docs, then follow the request path.",
    "body": (
        "Read the runtime first for lifecycle and delivery, then Sentinel for policy, "
        "audit, and observability on live MCP requests."
    ),
    "primary": {"label": "Runtime docs", "href": "/docs/runtime"},
    "secondary": {"label": "Sentinel docs", "href": "/docs/sentinel"},
}


@app.route("/")
def home():
    """Render the main marketing site."""
    return render_template(
        "index.html",
        nav_links=NAV_LINKS,
        hero=HERO,
        status=STATUS,
        product=PRODUCT,
        workflow=WORKFLOW,
        surfaces=SURFACES,
        docs_library=DOCS_LIBRARY,
        callout=CALLOUT,
    )


@app.route("/docs")
def docs_redirect():
    """Redirect the bare docs path to the canonical trailing-slash URL."""
    return redirect("/docs/")


@app.route("/docs/")
def docs_index():
    """Serve the docs landing page."""
    return send_from_directory("docs", "index.html")


@app.route("/docs/<path:page>")
def docs_page(page: str):
    """Serve a static docs page by path, accepting slash-terminated URLs."""
    page = page.rstrip("/")
    if not page.endswith(".html"):
        page = f"{page}.html"
    try:
        return send_from_directory("docs", page)
    except NotFound:
        abort(404)


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
