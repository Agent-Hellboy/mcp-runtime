from flask import Flask, abort, redirect, render_template, send_from_directory
from werkzeug.exceptions import NotFound

app = Flask(__name__)

NAV_LINKS = [
    {"label": "Platform", "href": "#product"},
    {"label": "Workflow", "href": "#workflow"},
    {"label": "Surfaces", "href": "#surfaces"},
    {"label": "Docs", "href": "#docs"},
]

HERO = {
    "eyebrow": "Open source Kubernetes platform for MCP",
    "title": "Operate MCP services with a higher-level platform control plane.",
    "subtitle": (
        "MCP Runtime sits above ingress and service-mesh layers. It standardizes "
        "MCP delivery, access, and rollout, while mcp-sentinel governs identity, "
        "policy, audit, and observability on the MCP request path."
    ),
    "primary": {"label": "Read the docs", "href": "/docs/"},
    "secondary": {"label": "See runtime guide", "href": "/docs/runtime"},
    "highlights": [
        "Self-hosted",
        "Higher-level than ingress or mesh",
        "Operator-managed",
    ],
    "proof_kicker": "MCP platform layer",
    "proof_title": "Above the network layer. Closer to the MCP lifecycle.",
    "proof_intro": (
        "Think of the runtime as the MCP operating layer on Kubernetes: it "
        "reconciles service delivery and access state, while Sentinel governs "
        "identity, policy, audit, and observability around live MCP requests."
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
    enabled: true
  analytics:
    enabled: true""",
    "stage_items": [
        {
            "label": "Runtime",
            "value": "MCP platform control plane",
            "body": "Owns bootstrap, reconciliation, rollout, ingress, and service lifecycle instead of acting as a generic network controller.",
        },
        {
            "label": "Access",
            "value": "Grants + sessions stay explicit",
            "body": "Keeps consent, trust ceilings, expiry, and revocation out of app-specific code.",
        },
        {
            "label": "Sentinel",
            "value": "Governed MCP request path",
            "body": "Places proxy enforcement, audit, and observability on MCP requests without claiming to be a mesh-wide data plane.",
        },
    ],
    "stage_signals": [
        "payments route admitted",
        "policy state synced",
        "audit and telemetry flowing",
    ],
}

STATUS = {
    "label": "Alpha platform",
    "body": (
        "The repo already covers runtime bootstrap, MCP deployment, access and "
        "session resources, gateway policy, and the bundled sentinel request path. "
        "This is a real MCP platform layer, not a generic service-mesh control plane, "
        "and the API surface is still alpha."
    ),
    "primary": {"label": "Read docs", "href": "/docs/"},
    "secondary": {"label": "View GitHub", "href": "https://github.com/Agent-Hellboy/mcp-runtime"},
}

SIGNALS = [
    {
        "label": "Control",
        "title": "One MCP platform control plane",
        "body": (
            "Use one resource contract for service definition, route, rollout, "
            "grants, and sessions instead of rebuilding deployment patterns for "
            "every MCP workload on top of lower-level cluster networking."
        ),
    },
    {
        "label": "Enforce",
        "title": "Policy runs on the request path",
        "body": (
            "Put identity extraction, trust checks, and allow or deny decisions in "
            "the proxy path so governance does not depend on each service "
            "implementation."
        ),
    },
    {
        "label": "Observe",
        "title": "Policy, audit, and observability ship with the platform",
        "body": (
            "Route audit and telemetry events into the sentinel ingest, processor, "
            "API, UI, and observability stack without stitching a separate product "
            "into every rollout."
        ),
    },
]

PRODUCT = {
    "title": "A higher-level MCP platform above ingress and service-mesh layers.",
    "intro": (
        "Use the control-plane term in the MCP sense: mcp-runtime owns lifecycle and "
        "cluster state, while mcp-sentinel governs the MCP request path with policy, "
        "audit, and operator visibility around live requests."
    ),
    "items": [
        {
            "label": "Runtime",
            "title": "mcp-runtime owns service definition, rollout, and cluster wiring",
            "body": (
                "Use one Kubernetes-native control plane for setup, registry, ingress, "
                "MCPServer reconciliation, and rollout behavior on top of the lower-level network stack."
            ),
            "points": [
                "Setup and cluster bootstrap",
                "Deployment / Service / Ingress reconciliation",
                "Stable service routes and rollout settings",
            ],
        },
        {
            "label": "Sentinel",
            "title": "mcp-sentinel owns policy, audit, observability, and the governed request path",
            "body": (
                "Run the proxy, gateway, ingest, processor, API, and UI path around "
                "MCP requests so governance is a platform capability, not an "
                "app-by-app patch."
            ),
            "points": [
                "Proxy sidecar on the request path",
                "Per-tool policy and trust evaluation",
                "Audit pipeline, UI, and observability stack",
            ],
        },
        {
            "label": "Access",
            "title": "Access and consent stay explicit instead of disappearing into app code",
            "body": (
                "Model who can call what, with which trust level, and for how long, "
                "using dedicated resources that evolve separately from deployment YAML."
            ),
            "points": [
                "MCPAccessGrant for entitlement",
                "MCPAgentSession for consent and expiry",
                "Revocation and trust ceilings",
            ],
        },
    ],
}

POSITIONING = {
    "title": "What this platform is, and what it is not.",
    "intro": (
        "This site uses control-plane language in an MCP-specific sense. The product "
        "is a higher-level operating layer on Kubernetes, not a generic network fabric."
    ),
    "items": [
        {
            "label": "Is",
            "title": "An MCP platform layer on Kubernetes",
            "body": (
                "It standardizes MCP service definition, rollout, ingress wiring, "
                "grants, sessions, and operator workflows for internal MCP services."
            ),
            "points": [
                "MCP-specific resource model",
                "Delivery + access lifecycle",
                "Operator-managed setup and rollout",
            ],
        },
        {
            "label": "Not",
            "title": "Not a general-purpose mesh or proxy control plane",
            "body": (
                "It does not try to replace lower-level ingress, gateway, or service-mesh "
                "infrastructure across every workload in the cluster."
            ),
            "points": [
                "Not a mesh-wide traffic fabric",
                "Not cluster networking for every workload",
                "Not a generic proxy fleet manager",
            ],
        },
        {
            "label": "Sits",
            "title": "Above ingress and service-mesh infrastructure",
            "body": (
                "If the cluster already has ingress, gateway, or mesh tooling, MCP "
                "Runtime should be understood as the MCP operating layer that sits on top."
            ),
            "points": [
                "Above ingress",
                "Above service mesh",
                "Focused on MCP request governance",
            ],
        },
    ],
}

WORKFLOW = [
    {
        "step": "01",
        "title": "Define the service and trust contract",
        "body": (
            "Describe image, route, gateway, analytics, and access expectations in a "
            "single runtime definition instead of separate hand-wired layers."
        ),
    },
    {
        "step": "02",
        "title": "Reconcile the control plane",
        "body": (
            "Use the CLI and operator to prepare cluster state, publish images, and "
            "reconcile the Kubernetes objects that expose the MCP service."
        ),
    },
    {
        "step": "03",
        "title": "Route through the governed traffic path",
        "body": (
            "Send live traffic through the proxy path when gateway mode is enabled so "
            "identity, policy, and audit happen on the request path."
        ),
    },
    {
        "step": "04",
        "title": "Inspect audit and iterate",
        "body": (
            "Use grants, sessions, and the sentinel surfaces to review behavior, tighten "
            "policy, and keep operators close to production reality."
        ),
    },
]

SURFACES = {
    "title": "What teams actually buy into",
    "intro": (
        "A credible platform needs a clear operating story across the MCP control "
        "plane, access model, request path, and docs."
    ),
    "items": [
        {
            "name": "Runtime",
            "path": "setup / cluster / operator / MCPServer",
            "body": "Own the control plane surface that prepares clusters and keeps MCP services reconciled.",
        },
        {
            "name": "Access",
            "path": "MCPAccessGrant / MCPAgentSession",
            "body": "Make entitlement, consent, and revocation explicit platform objects instead of app-side conventions.",
        },
        {
            "name": "Sentinel",
            "path": "proxy / gateway / ingest / processor / api / ui",
            "body": "Handle live MCP request governance, audit, and observability with a bundled request-path layer.",
        },
        {
            "name": "Docs",
            "path": "runtime / cli / sentinel / api",
            "body": "Explain the plane split clearly so operators can understand the architecture before they adopt it.",
        },
    ],
}

DOCS_LIBRARY = {
    "title": "Start with the plane you care about",
    "intro": (
        "Some teams begin with runtime operations. Others start at the governed "
        "request path. The docs are split the same way the platform is."
    ),
    "items": [
        {
            "tag": "Runtime",
            "title": "See the control plane and service lifecycle",
            "body": "Understand cluster bootstrap, MCPServer reconciliation, rollout, ingress, and how delivery state is modeled.",
            "href": "/docs/runtime",
            "label": "Runtime docs",
        },
        {
            "tag": "CLI",
            "title": "See the commands operators run day to day",
            "body": "Start with setup, cluster, server, registry, pipeline, and status workflows exposed by the CLI.",
            "href": "/docs/cli",
            "label": "CLI docs",
        },
        {
            "tag": "Sentinel",
            "title": "See the governed request path and observability layer",
            "body": "Review the bundled sentinel stack around proxy enforcement, audit events, query APIs, and observability.",
            "href": "/docs/sentinel",
            "label": "Sentinel docs",
        },
        {
            "tag": "API",
            "title": "See the contract behind both planes",
            "body": "Use the API reference for YAML examples, field semantics, gateway headers, resource status, and traffic semantics.",
            "href": "/docs/api",
            "label": "API reference",
        },
    ],
}

COMPANY = [
    {
        "label": "Platform engineering",
        "title": "Standardize MCP delivery",
        "body": "Give internal teams one paved road for MCP service rollout instead of a different Kubernetes pattern per workload.",
    },
    {
        "label": "Security and governance",
        "title": "Move policy onto the request path",
        "body": "Apply trust, identity, consent, and audit at a platform layer rather than relying on every service to implement it.",
    },
    {
        "label": "AI infrastructure",
        "title": "Operate governed MCP traffic at cluster level",
        "body": "Run MCP integrations with a consistent control plane and traffic policy model across clusters your org already owns.",
    },
]

CALLOUT = {
    "title": "Start with the two-plane architecture.",
    "body": (
        "Runtime docs explain lifecycle and delivery. Sentinel docs explain traffic, "
        "policy, and audit. Together they tell the actual platform story."
    ),
    "primary": {"label": "Runtime docs", "href": "/docs/runtime"},
    "secondary": {"label": "Sentinel docs", "href": "/docs/sentinel"},
}


@app.route("/")
def home():
    return render_template(
        "index.html",
        nav_links=NAV_LINKS,
        hero=HERO,
        status=STATUS,
        signals=SIGNALS,
        product=PRODUCT,
        positioning=POSITIONING,
        workflow=WORKFLOW,
        surfaces=SURFACES,
        docs_library=DOCS_LIBRARY,
        company=COMPANY,
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
