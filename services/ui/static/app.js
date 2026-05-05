const apiBase = window.MCP_API_BASE || "/api";
const defaults = Object.assign(
  { namespace: "mcp-servers", policyVersion: "v1" },
  window.MCP_DEFAULTS || {}
);
let authenticated = null;
let authPrincipal = null;
let grantsCache = [];
let sessionsCache = [];
let userAPIKeysCache = [];
let serversCache = [];
let userAPIKeyClearTimer = null;
let serverSearchQuery = "";
let serverStatusFilter = "all";

// API Helper
async function fetchJSON(path, options = {}) {
  const headers = { ...options.headers };

  const response = await fetch(`${apiBase}${path}`, {
    ...options,
    credentials: "same-origin",
    headers,
  });

  if (!response.ok) {
    const error = await response.text();
    if (response.status === 401) {
      setAuthenticated(false);
      showAuthModal("Sign in to continue.");
      throw unauthorizedError();
    }
    throw new Error(error || `Request failed: ${response.status}`);
  }

  return response.json();
}

function unauthorizedError() {
  const err = new Error("Unauthorized");
  err.name = "UnauthorizedError";
  return err;
}

function isUnauthorizedError(err) {
  return err?.name === "UnauthorizedError";
}

async function copyTextToClipboard(text, successMessage = "Copied") {
  const value = String(text || "");
  if (!value) {
    showToast("Nothing to copy", "warning");
    return;
  }

  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value);
    } else {
      const textarea = document.createElement("textarea");
      textarea.value = value;
      textarea.setAttribute("readonly", "");
      textarea.style.position = "fixed";
      textarea.style.left = "-9999px";
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand("copy");
      textarea.remove();
    }
    showToast(successMessage);
  } catch (err) {
    console.error("Failed to copy text:", err);
    showToast("Copy failed", "error");
  }
}

// Toast Notifications
function showToast(message, type = "success") {
  const container = document.getElementById("toasts");
  if (!container) {
    return;
  }

  const safeType = ["success", "error", "warning"].includes(type)
    ? type
    : "success";
  const toast = document.createElement("div");
  toast.className = `toast ${safeType}`;

  const text = document.createElement("span");
  text.className = "toast-message";
  text.textContent = String(message);

  const close = document.createElement("button");
  close.className = "toast-close";
  close.type = "button";
  close.setAttribute("aria-label", "Dismiss notification");
  close.textContent = "×";
  close.addEventListener("click", () => {
    toast.remove();
  });

  toast.append(text, close);
  container.appendChild(toast);

  setTimeout(() => {
    toast.remove();
  }, 5000);
}

// Tab Switching
function initTabs() {
  const tabs = document.querySelectorAll(".tab");

  tabs.forEach((tab) => {
    tab.addEventListener("click", () => {
      const target = tab.dataset.tab;

      if (authenticated !== true && target !== "servers") {
        if (authenticated === false) {
          showAuthModal();
        }
        return;
      }

      activateTab(target);

      // Load data when switching to certain tabs
      if (target === "dashboard") {
        loadDashboardSummary();
        loadDashboardAnalytics();
        loadEvents();
      } else if (target === "governance") {
        loadGrants();
        loadSessions();
      } else if (target === "operations") {
        loadComponents();
      } else if (target === "userkeys") {
        loadUserAPIKeys();
      } else if (target === "servers") {
        loadServers();
      }
    });
  });
}

// Dashboard
let autoRefreshInterval = null;

async function loadDashboardSummary() {
  try {
    const data = await fetchJSON("/dashboard/summary");

    document.getElementById("dash-total-events").textContent = formatNumber(
      data.total_events || 0
    );
    document.getElementById("dash-active-servers").textContent =
      data.active_servers || 0;
    document.getElementById("dash-active-grants").textContent =
      data.active_grants || 0;
    document.getElementById("dash-active-sessions").textContent =
      data.active_sessions || 0;
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load dashboard summary:", err);
  }
}

async function loadDashboardAnalytics() {
  try {
    const limit = document.getElementById("analytics-limit")?.value || "10";
    const data = await fetchJSON(`/analytics/usage?limit=${encodeURIComponent(limit)}`);
    renderDashboardAnalytics(data);
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load dashboard analytics:", err);
    renderAnalyticsError();
  }
}

function renderDashboardAnalytics(data) {
  const totals = data?.totals || {};
  const events = Number(totals.events || 0);
  const allowed = Number(totals.allowed || 0);
  const denied = Number(totals.denied || 0);
  setText("analytics-total-events", formatNumber(events));
  setText("analytics-allowed", formatNumber(allowed));
  setText("analytics-denied", formatNumber(denied));
  setText("analytics-humans", formatNumber(totals.unique_humans || 0));
  setText("analytics-agents", formatNumber(totals.unique_agents || 0));
  renderDecisionMeter(events, allowed, denied);

  renderAnalyticsServers(data?.servers || []);
  renderAnalyticsActors(data?.actors || []);
  renderAnalyticsTools(data?.tools || []);
  renderAnalyticsDecisions(data?.decisions || []);
}

function renderDecisionMeter(events, allowed, denied) {
  const total = Math.max(Number(events || 0), allowed + denied);
  const allowRate = total > 0 ? Math.round((allowed / total) * 100) : 0;
  const denyRate = total > 0 ? Math.round((denied / total) * 100) : 0;
  const allowEl = document.getElementById("decision-meter-allow");
  const denyEl = document.getElementById("decision-meter-deny");
  if (allowEl) allowEl.style.width = `${allowRate}%`;
  if (denyEl) denyEl.style.width = `${denyRate}%`;
  setText("analytics-allow-rate", total > 0 ? `${allowRate}% allow` : "-");
  setText("analytics-deny-rate", total > 0 ? `${denyRate}%` : "-");
}

function renderAnalyticsServers(rows) {
  const tbody = document.getElementById("analytics-servers-body");
  if (!tbody) return;
  if (!rows.length) {
    tbody.innerHTML = '<tr><td colspan="7" class="empty">No usage yet.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  rows.forEach((item) => {
    const row = document.createElement("tr");
    row.appendChild(createTextCell(item.server || "-"));
    row.appendChild(createTextCell(item.namespace || "-"));
    row.appendChild(createTextCell(formatNumber(item.events || 0)));
    row.appendChild(createTextCell(formatNumber(item.allowed || 0)));
    row.appendChild(createTextCell(formatNumber(item.denied || 0)));
    row.appendChild(createTextCell(formatNumber(item.unique_humans || 0)));
    row.appendChild(createTextCell(formatNumber(item.unique_agents || 0)));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function renderAnalyticsActors(rows) {
  const tbody = document.getElementById("analytics-actors-body");
  if (!tbody) return;
  if (!rows.length) {
    tbody.innerHTML = '<tr><td colspan="6" class="empty">No actors yet.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  rows.forEach((item) => {
    const row = document.createElement("tr");
    row.appendChild(createTextCell(item.human_id || "-"));
    row.appendChild(createTextCell(item.agent_id || "-"));
    row.appendChild(createTextCell(formatNumber(item.events || 0)));
    row.appendChild(createTextCell(formatNumber(item.unique_servers || 0)));
    row.appendChild(createTextCell(formatNumber(item.unique_tools || 0)));
    row.appendChild(createTextCell(formatNumber(item.denied || 0)));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function renderAnalyticsTools(rows) {
  const tbody = document.getElementById("analytics-tools-body");
  if (!tbody) return;
  if (!rows.length) {
    tbody.innerHTML = '<tr><td colspan="4" class="empty">No tool calls yet.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  rows.forEach((item) => {
    const row = document.createElement("tr");
    row.appendChild(createTextCell(item.server || "-"));
    row.appendChild(createTextCell(item.tool_name || "-"));
    row.appendChild(createTextCell(formatNumber(item.events || 0)));
    row.appendChild(createTextCell(formatNumber(item.denied || 0)));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function renderAnalyticsDecisions(rows) {
  const tbody = document.getElementById("analytics-decisions-body");
  if (!tbody) return;
  if (!rows.length) {
    tbody.innerHTML = '<tr><td colspan="2" class="empty">No decisions yet.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  rows.forEach((item) => {
    const row = document.createElement("tr");
    row.appendChild(createBadgeCell(item.decision || "unknown", decisionBadgeClass(item.decision)));
    row.appendChild(createTextCell(formatNumber(item.events || 0)));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function decisionBadgeClass(decision) {
  if (decision === "allow") return "badge-success";
  if (decision === "deny") return "badge-error";
  return "badge-muted";
}

function renderAnalyticsError() {
  setText("analytics-total-events", "-");
  setText("analytics-allowed", "-");
  setText("analytics-denied", "-");
  setText("analytics-humans", "-");
  setText("analytics-agents", "-");
  renderDecisionMeter(0, 0, 0);
  document.getElementById("analytics-servers-body").innerHTML =
    '<tr><td colspan="7" class="empty">Error loading usage.</td></tr>';
  document.getElementById("analytics-actors-body").innerHTML =
    '<tr><td colspan="6" class="empty">Error loading actors.</td></tr>';
  document.getElementById("analytics-tools-body").innerHTML =
    '<tr><td colspan="4" class="empty">Error loading tools.</td></tr>';
  document.getElementById("analytics-decisions-body").innerHTML =
    '<tr><td colspan="2" class="empty">Error loading decisions.</td></tr>';
}

function formatNumber(num) {
  if (num >= 1000000) {
    return (num / 1000000).toFixed(1) + "M";
  }
  if (num >= 1000) {
    const roundedThousands = Math.round((num / 1000) * 10) / 10;
    if (roundedThousands >= 1000) {
      return (num / 1000000).toFixed(1) + "M";
    }
    return roundedThousands.toFixed(1) + "K";
  }
  return num.toString();
}

async function loadEvents() {
  try {
    const limit = 50;
    const data = await fetchJSON(`/events?limit=${limit}`);
    const tbody = document.getElementById("events-body");

    if (!data.events || data.events.length === 0) {
      tbody.innerHTML =
        '<tr><td colspan="6" class="empty">No events yet.</td></tr>';
      return;
    }

    tbody.innerHTML = "";
    const fragment = document.createDocumentFragment();

    data.events.forEach((event) => {
      const row = document.createElement("tr");
      row.innerHTML = `
        <td>${renderAuditTime(event)}</td>
        <td>${renderAuditIdentity("Human", event.human_id)}</td>
        <td>${renderAuditIdentity("Agent", event.agent_id)}</td>
        <td>${renderAuditTarget(event)}</td>
        <td>${renderDecision(event.decision)}</td>
        <td>${renderPolicySummary(event)}</td>
      `;
      fragment.appendChild(row);
    });

    tbody.appendChild(fragment);
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load events:", err);
  }
}

function renderAuditTime(event) {
  const timestamp = event.timestamp ? new Date(event.timestamp).toLocaleString() : "-";
  const source = event.source || event.event_type
    ? [event.source, event.event_type].filter(Boolean).join(" / ")
    : "";
  return `
    <div class="audit-cell">
      <strong>${escapeHtml(timestamp)}</strong>
      ${source ? `<span>${escapeHtml(source)}</span>` : ""}
    </div>
  `;
}

function renderAuditIdentity(label, value) {
  if (!value) return '<span class="muted-text">-</span>';
  return `
    <span class="subject-chip">
      <span class="subject-chip-label">${escapeHtml(label)}</span>
      <strong>${escapeHtml(value)}</strong>
    </span>
  `;
}

function renderAuditTarget(event) {
  const payload = event.payload || {};
  const server = event.server || payload.server || "-";
  const namespace = event.namespace || payload.namespace || "";
  const action = event.tool_name || payload.tool_name || payload.rpc_method || event.event_type || "-";
  return `
    <div class="audit-cell">
      <strong>${escapeHtml(server)}</strong>
      ${namespace ? `<span>${escapeHtml(namespace)}</span>` : ""}
      <span class="audit-action">${escapeHtml(action)}</span>
    </div>
  `;
}

function renderDecision(decision) {
  if (!decision) return '<span class="muted-text">-</span>';
  return `<span class="badge ${decisionBadgeClass(decision)}">${escapeHtml(decision)}</span>`;
}

function renderPolicySummary(event) {
  const payload = event.payload || {};
  const reason = payload.reason || event.decision || "-";
  const trustParts = [
    payload.effective_trust ? `effective ${payload.effective_trust}` : "",
    payload.required_trust ? `required ${payload.required_trust}` : "",
  ].filter(Boolean);
  const session = event.session_id || payload.session_id || "";
  return `
    <div class="audit-cell">
      <strong>${escapeHtml(reason)}</strong>
      ${trustParts.length ? `<span>${escapeHtml(trustParts.join(" / "))}</span>` : ""}
      ${session ? `<span>session ${escapeHtml(session)}</span>` : ""}
    </div>
  `;
}

function escapeHtml(text) {
  if (!text) return "";
  const div = document.createElement("div");
  div.textContent = text;
  return div.innerHTML;
}

function setText(id, text) {
  const el = document.getElementById(id);
  if (el) el.textContent = text;
}

function encodePathSegment(value) {
  return encodeURIComponent(String(value));
}

function debounce(fn, waitMs) {
  let timeoutId = null;
  return (...args) => {
    if (timeoutId) {
      clearTimeout(timeoutId);
    }
    timeoutId = setTimeout(() => {
      timeoutId = null;
      fn(...args);
    }, waitMs);
  };
}

function createTextCell(text) {
  const cell = document.createElement("td");
  cell.textContent = text;
  return cell;
}

function createBadge(text, className) {
  const badge = document.createElement("span");
  badge.className = `badge ${className}`;
  badge.textContent = text;
  return badge;
}

function createBadgeCell(text, className) {
  const cell = document.createElement("td");
  cell.appendChild(createBadge(text, className));
  return cell;
}

function createIdentityCell(primary, secondary = "") {
  const cell = document.createElement("td");
  const stack = document.createElement("div");
  stack.className = "identity-stack";

  const primaryEl = document.createElement("strong");
  primaryEl.className = "identity-primary";
  primaryEl.textContent = primary || "-";
  stack.appendChild(primaryEl);

  if (secondary) {
    const secondaryEl = document.createElement("span");
    secondaryEl.className = "identity-secondary";
    secondaryEl.textContent = secondary;
    stack.appendChild(secondaryEl);
  }

  cell.appendChild(stack);
  return cell;
}

function createSubjectCell(subject = {}) {
  const cell = document.createElement("td");
  const chips = document.createElement("div");
  chips.className = "subject-chip-list";

  if (subject.humanID) {
    chips.appendChild(createSubjectChip("Human", subject.humanID));
  }
  if (subject.agentID) {
    chips.appendChild(createSubjectChip("Agent", subject.agentID));
  }
  if (!chips.children.length) {
    chips.textContent = "-";
  }

  cell.appendChild(chips);
  return cell;
}

function createSubjectChip(label, value) {
  const chip = document.createElement("span");
  chip.className = "subject-chip";

  const labelEl = document.createElement("span");
  labelEl.className = "subject-chip-label";
  labelEl.textContent = label;

  const valueEl = document.createElement("strong");
  valueEl.textContent = value;

  chip.append(labelEl, valueEl);
  return chip;
}

function createTrustCell(trust) {
  const value = trust || "-";
  return createBadgeCell(value, trustBadgeClass(value));
}

function trustBadgeClass(trust) {
  if (trust === "high") return "badge-trust-high";
  if (trust === "medium") return "badge-trust-medium";
  if (trust === "low") return "badge-trust-low";
  return "badge-muted";
}

function createActionCell(label, onClick) {
  const cell = document.createElement("td");
  const button = document.createElement("button");
  button.type = "button";
  button.className = "ghost action-btn";
  button.textContent = label;
  button.addEventListener("click", onClick);
  cell.appendChild(button);
  return cell;
}

// Authentication
async function initAuth() {
  document.getElementById("auth-form")?.addEventListener("submit", handleAuthSubmit);
  document.getElementById("auth-open")?.addEventListener("click", () => {
    showAuthModal();
  });
  document.getElementById("auth-logout")?.addEventListener("click", logout);
  initGoogleSignIn();

  try {
    const response = await fetch("/auth/status", { credentials: "same-origin" });
    const data = await response.json();
    authPrincipal = data?.principal || null;
    setAuthenticated(Boolean(data.authenticated));
  } catch (err) {
    console.error("Failed to check auth status:", err);
    authPrincipal = null;
    setAuthenticated(false);
  }

  if (authenticated) {
    loadActiveTab();
    startAutoRefresh();
  } else {
    activateTab("servers");
    loadServers();
  }
}

async function handleAuthSubmit(event) {
  event.preventDefault();
  const apiKeyInput = document.getElementById("api-key-input");
  const emailInput = document.getElementById("auth-email-input");
  const passwordInput = document.getElementById("auth-password-input");
  const submit = document.getElementById("auth-submit");
  const apiKey = apiKeyInput?.value || "";
  const email = emailInput?.value || "";
  const password = passwordInput?.value || "";

  setAuthError("");
  if ((email && !password) || (!email && password)) {
    setAuthError("Enter both email and password, or use an API key.");
    return;
  }
  if (!email && !password && !apiKey) {
    setAuthError("Provide email and password, an API key, or sign in with Google.");
    return;
  }
  if (submit) submit.disabled = true;
  try {
    const payload =
      email || password ? { email, password } : { api_key: apiKey };
    const data = await performLogin(payload);
    authPrincipal = data?.principal || null;
    if (apiKeyInput) apiKeyInput.value = "";
    if (passwordInput) passwordInput.value = "";
    hideAuthModal();
    setAuthenticated(true);
    loadActiveTab();
    startAutoRefresh();
  } catch (err) {
    setAuthError(err.message);
  } finally {
    if (submit) submit.disabled = false;
  }
}

function initGoogleSignIn(attempt = 0) {
  const clientID = window.MCP_GOOGLE_CLIENT_ID || "";
  if (!clientID) {
    return;
  }
  const container = document.getElementById("google-signin");
  if (!container) {
    return;
  }
  if (!window.google?.accounts?.id) {
    if (attempt < 20) {
      setTimeout(() => initGoogleSignIn(attempt + 1), 250);
    }
    return;
  }
  window.google.accounts.id.initialize({
    client_id: clientID,
    callback: handleGoogleSignIn,
  });
  container.innerHTML = "";
  window.google.accounts.id.renderButton(container, {
    theme: "outline",
    size: "large",
    shape: "pill",
    text: "continue_with",
    width: 280,
  });
}

async function handleGoogleSignIn(response) {
  const token = response?.credential || "";
  if (!token) {
    setAuthError("Google sign-in did not return a token.");
    return;
  }
  setAuthError("");
  try {
    const data = await performLogin({ id_token: token });
    authPrincipal = data?.principal || null;
    hideAuthModal();
    setAuthenticated(true);
    loadActiveTab();
    startAutoRefresh();
  } catch (err) {
    setAuthError(err.message);
  }
}

async function performLogin(payload) {
  const response = await fetch("/auth/login", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    throw new Error(await authFailureMessage(response));
  }
  return response.json();
}

async function authFailureMessage(response) {
  let serverError = "";
  try {
    const body = await response.json();
    serverError = body?.error || "";
  } catch (_) {
    // Non-JSON failures still get a useful status-based message below.
  }

  if (response.status === 401) {
    return "Invalid credentials";
  }
  if (response.status === 503 && serverError === "api_key_not_configured") {
    return "Server is not configured for API key auth";
  }
  if (response.status === 400 && serverError === "missing_credentials") {
    return "Provide email and password, an API key, or sign in with Google.";
  }
  return serverError || `Sign-in failed (${response.status})`;
}

async function logout() {
  try {
    await fetch("/auth/logout", {
      method: "POST",
      credentials: "same-origin",
    });
  } catch (err) {
    console.error("Failed to sign out:", err);
  }
  stopAutoRefresh();
  authPrincipal = null;
  setAuthenticated(false);
  resetDashboard();
  resetGovernance();
  resetUserAPIKeys();
  activateTab("servers");
  loadServers();
}

function setAuthenticated(value) {
  authenticated = value;
  const role = authPrincipal?.role || "";
  const roleLabel = role ? `Role: ${role}` : "";
  const roleEl = document.getElementById("auth-role");
  if (roleEl) {
    roleEl.textContent = roleLabel;
    roleEl.classList.toggle("hidden", !value || !roleLabel);
  }
  document.getElementById("auth-open")?.classList.toggle("hidden", value);
  document.getElementById("auth-logout")?.classList.toggle("hidden", !value);
  applyRoleVisibility();
}

function showAuthModal(message = "") {
  stopAutoRefresh();
  setAuthError(message);
  const modal = document.getElementById("auth-modal");
  modal?.classList.remove("hidden");
  setTimeout(() => document.getElementById("auth-email-input")?.focus(), 0);
}

function hideAuthModal() {
  document.getElementById("auth-modal")?.classList.add("hidden");
  setAuthError("");
}

function setAuthError(message) {
  const error = document.getElementById("auth-error");
  if (!error) return;
  error.textContent = message;
  error.classList.toggle("hidden", !message);
}

function loadActiveTab() {
  if (!authenticated) return;
  const active = resolveActiveTab();
  if (active === "dashboard") {
    loadDashboardSummary();
    loadDashboardAnalytics();
    loadEvents();
  } else if (active === "servers") {
    loadServers();
  } else if (active === "governance") {
    loadGrants();
    loadSessions();
  } else if (active === "operations") {
    loadComponents();
  } else if (active === "userkeys") {
    loadUserAPIKeys();
  }
}

function resetDashboard() {
  setText("dash-total-events", "-");
  setText("dash-active-servers", "-");
  setText("dash-active-grants", "-");
  setText("dash-active-sessions", "-");
  setText("analytics-total-events", "-");
  setText("analytics-allowed", "-");
  setText("analytics-denied", "-");
  setText("analytics-humans", "-");
  setText("analytics-agents", "-");
  renderDecisionMeter(0, 0, 0);
  document.getElementById("analytics-servers-body").innerHTML =
    '<tr><td colspan="7" class="empty">No usage yet.</td></tr>';
  document.getElementById("analytics-actors-body").innerHTML =
    '<tr><td colspan="6" class="empty">No actors yet.</td></tr>';
  document.getElementById("analytics-tools-body").innerHTML =
    '<tr><td colspan="4" class="empty">No tool calls yet.</td></tr>';
  document.getElementById("analytics-decisions-body").innerHTML =
    '<tr><td colspan="2" class="empty">No decisions yet.</td></tr>';
  document.getElementById("events-body").innerHTML =
    '<tr><td colspan="6" class="empty">No events yet.</td></tr>';
}

function resetGovernance() {
  grantsCache = [];
  sessionsCache = [];
  renderGovernanceSummary();
  const grantsBody = document.getElementById("grants-body");
  const sessionsBody = document.getElementById("sessions-body");
  if (grantsBody) {
    grantsBody.innerHTML = '<tr><td colspan="6" class="empty">No grants found.</td></tr>';
  }
  if (sessionsBody) {
    sessionsBody.innerHTML = '<tr><td colspan="6" class="empty">No sessions found.</td></tr>';
  }
}

async function loadServers() {
  try {
    const data = await fetchJSON("/runtime/servers");
    serversCache = Array.isArray(data.servers) ? data.servers : [];
    renderServers();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load servers:", err);
    serversCache = [];
    const grid = document.getElementById("servers-grid");
    if (grid) {
      grid.innerHTML = '<div class="component-card error">Error loading MCP servers.</div>';
    }
  }
}

function renderServers() {
  const grid = document.getElementById("servers-grid");
  if (!grid) return;
  renderServerCatalogSummary();

  if (serversCache.length === 0) {
    grid.innerHTML = '<div class="server-empty-state">No MCP servers found.</div>';
    return;
  }

  const servers = filteredServers();
  if (servers.length === 0) {
    grid.innerHTML = '<div class="server-empty-state">No servers match this search.</div>';
    return;
  }

  grid.innerHTML = "";
  const fragment = document.createDocumentFragment();
  servers.forEach((server) => {
    const card = document.createElement("article");
    card.className = "server-card";

    card.appendChild(renderServerHero(server));
    card.appendChild(renderServerMeta(server));
    card.appendChild(renderServerEndpoint(server));

    const inventory = document.createElement("div");
    inventory.className = "server-inventory-grid";
    inventory.appendChild(renderInventoryBlock("Tools", server.tools || [], renderToolItem));
    inventory.appendChild(renderInventoryBlock("Prompts", server.prompts || [], renderInventoryItem));
    inventory.appendChild(renderInventoryBlock("Resources", server.resources || [], renderInventoryItem));
    inventory.appendChild(renderInventoryBlock("Tasks", server.tasks || [], renderInventoryItem));
    card.appendChild(inventory);

    if (server.access_json && Object.keys(server.access_json).length) {
      card.appendChild(renderServerConnectConfig(server));
    }

    fragment.appendChild(card);
  });
  grid.appendChild(fragment);
}

function filteredServers() {
  const query = serverSearchQuery.trim().toLowerCase();
  return serversCache.filter((server) => {
    const status = String(server.status || "Unknown").toLowerCase();
    if (serverStatusFilter === "ready" && status !== "ready") return false;
    if (serverStatusFilter === "attention" && status === "ready") return false;
    if (!query) return true;
    return serverSearchText(server).includes(query);
  });
}

function serverSearchText(server) {
  const values = [
    server.name,
    server.namespace,
    server.status,
    server.ready,
    server.endpoint,
    ...(server.tools || []).map((tool) => `${tool.name || ""} ${tool.requiredTrust || ""} ${tool.description || ""}`),
    ...(server.prompts || []).map(inventorySearchText),
    ...(server.resources || []).map(inventorySearchText),
    ...(server.tasks || []).map(inventorySearchText),
  ];
  return values.filter(Boolean).join(" ").toLowerCase();
}

function inventorySearchText(item) {
  if (typeof item === "string") return item;
  const labels =
    item?.labels && typeof item.labels === "object"
      ? Object.entries(item.labels)
          .map(([k, v]) => `${k} ${v}`)
          .join(" ")
      : "";
  return `${item?.name || ""} ${item?.description || ""} ${labels}`;
}

function renderServerCatalogSummary() {
  const total = serversCache.length;
  const ready = serversCache.filter((server) => server.status === "Ready").length;
  const tools = serversCache.reduce((sum, server) => sum + (server.tools || []).length, 0);
  setText("server-count-total", formatNumber(total));
  setText("server-count-ready", formatNumber(ready));
  setText("server-count-tools", formatNumber(tools));
}

function renderServerHero(server) {
  const hero = document.createElement("div");
  hero.className = "server-card-hero";
  hero.innerHTML = `
    <div class="server-identity">
      <span class="server-avatar" aria-hidden="true">${escapeHtml(serverInitials(server.name))}</span>
      <div class="server-title-stack">
        <h3>${escapeHtml(server.name || "-")}</h3>
        <p>${escapeHtml(server.namespace || "-")}</p>
      </div>
    </div>
    <div class="server-status-stack">
      <span class="badge ${serverBadgeClass(server.status)}">${escapeHtml(server.status || "Unknown")}</span>
      <span>${escapeHtml(server.ready || "0/0")} pods</span>
    </div>
  `;
  return hero;
}

function renderServerMeta(server) {
  const wrap = document.createElement("div");
  wrap.className = "server-meta-wrap";

  const meta = document.createElement("div");
  meta.className = "server-meta-row";
  meta.appendChild(serverMetaPill("HTTP MCP"));
  meta.appendChild(serverMetaPill(`${(server.tools || []).length} tools`));
  meta.appendChild(serverMetaPill(`${(server.prompts || []).length} prompts`));
  meta.appendChild(serverMetaPill(`${(server.resources || []).length} resources`));
  meta.appendChild(serverMetaPill(`Created ${formatServerAge(server.age)}`));
  wrap.appendChild(meta);

  const actions = document.createElement("div");
  actions.className = "server-card-actions";
  if (server.endpoint) {
    const copyURL = document.createElement("button");
    copyURL.className = "ghost server-action";
    copyURL.type = "button";
    copyURL.textContent = "Copy URL";
    copyURL.addEventListener("click", () => copyTextToClipboard(server.endpoint, "Endpoint copied"));
    actions.appendChild(copyURL);
  }
  if (server.access_json && Object.keys(server.access_json).length) {
    const jsonText = JSON.stringify(server.access_json || {}, null, 2);
    const copyJSON = document.createElement("button");
    copyJSON.className = "ghost server-action";
    copyJSON.type = "button";
    copyJSON.textContent = "Copy JSON";
    copyJSON.addEventListener("click", () => copyTextToClipboard(jsonText, "Connect JSON copied"));
    actions.appendChild(copyJSON);
  }
  if (actions.children.length) {
    wrap.appendChild(actions);
  }
  return wrap;
}

function serverMetaPill(text) {
  const item = document.createElement("span");
  item.className = "server-meta-pill";
  item.textContent = text;
  return item;
}

function renderServerEndpoint(server) {
  const row = document.createElement("div");
  row.className = "server-endpoint-row";
  const endpoint = document.createElement("code");
  endpoint.className = "server-endpoint";
  endpoint.textContent = server.endpoint || "No public endpoint";
  row.appendChild(endpoint);

  return row;
}

function renderServerConnectConfig(server) {
  const details = document.createElement("details");
  details.className = "server-connect";
  const summary = document.createElement("summary");
  summary.textContent = "Connect config";
  details.appendChild(summary);

  const body = document.createElement("div");
  body.className = "server-connect-body";
  const json = document.createElement("pre");
  json.className = "access-json";
  const jsonText = JSON.stringify(server.access_json || {}, null, 2);
  json.textContent = jsonText;
  body.appendChild(json);
  details.appendChild(body);
  return details;
}

function serverInitials(name) {
  return String(name || "MCP")
    .split(/[-_\s]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0])
    .join("")
    .toUpperCase();
}

function serverBadgeClass(status) {
  if (status === "Ready") return "badge-success";
  if (status === "Degraded") return "badge-warning";
  if (status === "NotReady") return "badge-error";
  return "badge-muted";
}

function formatServerAge(value) {
  if (!value) return "-";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function setText(id, value) {
  const node = document.getElementById(id);
  if (node) node.textContent = value;
}

function renderInventoryBlock(label, items, itemRenderer) {
  const block = document.createElement("div");
  block.className = "inventory-block";
  const heading = document.createElement("h4");
  heading.textContent = label;
  block.appendChild(heading);
  if (!items.length) {
    const empty = document.createElement("p");
    empty.className = "inventory-empty";
    empty.textContent = "None";
    block.appendChild(empty);
    return block;
  }
  const list = document.createElement("ul");
  items.forEach((item) => {
    const li = document.createElement("li");
    li.innerHTML = itemRenderer(item);
    list.appendChild(li);
  });
  block.appendChild(list);
  return block;
}

function renderToolItem(tool) {
  const trust = tool.requiredTrust ? ` <span class="trust-chip">${escapeHtml(tool.requiredTrust)}</span>` : "";
  const desc = tool.description ? `<small>${escapeHtml(tool.description)}</small>` : "";
  return `<strong>${escapeHtml(tool.name || "-")}</strong>${trust}${desc}`;
}

function renderInventoryItem(item) {
  if (typeof item === "string") {
    return `<strong>${escapeHtml(item || "-")}</strong>`;
  }
  const name = item?.name || "-";
  const desc = item?.description ? `<small>${escapeHtml(item.description)}</small>` : "";
  const labels = item?.labels && typeof item.labels === "object" ? Object.entries(item.labels) : [];
  const labelsText = labels.length
    ? `<small>${escapeHtml(labels.map(([k, v]) => `${k}=${v}`).join(", "))}</small>`
    : "";
  return `<strong>${escapeHtml(name)}</strong>${desc}${labelsText}`;
}

function initDashboard() {
  // Auto refresh
  const autoRefreshCheckbox = document.getElementById("auto-refresh");
  if (autoRefreshCheckbox) {
    autoRefreshCheckbox.addEventListener("change", (e) => {
      if (e.target.checked) {
        startAutoRefresh();
      } else {
        stopAutoRefresh();
      }
    });
  }

  document.getElementById("refresh-events")?.addEventListener("click", () => {
    loadEvents();
  });
  document.getElementById("refresh-analytics")?.addEventListener("click", () => {
    loadDashboardAnalytics();
  });
  document.getElementById("analytics-limit")?.addEventListener("change", () => {
    loadDashboardAnalytics();
  });
  document.getElementById("refresh-servers")?.addEventListener("click", () => {
    loadServers();
  });
  document.getElementById("server-search")?.addEventListener("input", (event) => {
    serverSearchQuery = event.target.value || "";
    renderServers();
  });
  document.querySelectorAll("[data-server-status]").forEach((button) => {
    button.addEventListener("click", () => {
      serverStatusFilter = button.dataset.serverStatus || "all";
      document.querySelectorAll("[data-server-status]").forEach((node) => {
        node.classList.toggle("active", node === button);
      });
      renderServers();
    });
  });
}

function startAutoRefresh() {
  if (!authenticated) return;
  if (authPrincipal?.role !== "admin") return;
  if (autoRefreshInterval) return;
  const autoRefreshCheckbox = document.getElementById("auto-refresh");
  if (autoRefreshCheckbox && !autoRefreshCheckbox.checked) return;
  autoRefreshInterval = setInterval(() => {
    loadDashboardSummary();
    loadDashboardAnalytics();
    loadEvents();
  }, 5000);
}

function isAdminUser() {
  return authPrincipal?.role === "admin";
}

function applyRoleVisibility() {
  const adminOnly = document.querySelectorAll('[data-admin-only="true"]');
  adminOnly.forEach((node) => {
    node.classList.toggle("hidden", !isAdminUser());
  });
  const active = resolveActiveTab();
  activateTab(active);
}

function resolveActiveTab() {
  const active = document.querySelector(".tab.active")?.dataset.tab;
  if (active && (isAdminUser() || active === "userkeys" || active === "servers")) {
    return active;
  }
  return "servers";
}

function activateTab(target) {
  const tabs = document.querySelectorAll(".tab");
  const contents = document.querySelectorAll(".tab-content");
  tabs.forEach((t) => {
    const isActive = t.dataset.tab === target && !t.classList.contains("hidden");
    t.classList.toggle("active", isActive);
    t.setAttribute("aria-selected", String(isActive));
  });
  contents.forEach((content) => {
    const isActive = content.id === `tab-${target}` && !content.classList.contains("hidden");
    content.classList.toggle("active", isActive);
    content.hidden = !isActive;
  });
}

function stopAutoRefresh() {
  if (autoRefreshInterval) {
    clearInterval(autoRefreshInterval);
    autoRefreshInterval = null;
  }
}

// Governance - Grants
async function loadGrants() {
  try {
    const data = await fetchJSON("/runtime/grants");
    grantsCache = Array.isArray(data.grants) ? data.grants : [];
    renderGovernanceSummary();
    renderGrants();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load grants:", err);
    grantsCache = [];
    renderGovernanceSummary();
    document.getElementById("grants-body").innerHTML =
      '<tr><td colspan="6" class="empty">Error loading grants.</td></tr>';
  }
}

function renderGovernanceSummary() {
  const activeGrants = grantsCache.filter((grant) => !grant.disabled).length;
  const disabledGrants = grantsCache.length - activeGrants;
  const activeSessions = sessionsCache.filter((session) => !session.revoked).length;
  const revokedSessions = sessionsCache.length - activeSessions;

  setText("gov-active-grants", formatNumber(activeGrants));
  setText("gov-disabled-grants", formatNumber(disabledGrants));
  setText("gov-active-sessions", formatNumber(activeSessions));
  setText("gov-revoked-sessions", formatNumber(revokedSessions));
}

function renderGrants() {
  const tbody = document.getElementById("grants-body");
  if (!tbody) return;
  const filter = document.getElementById("grant-filter")?.value.toLowerCase() || "";

  if (grantsCache.length === 0) {
    tbody.innerHTML = '<tr><td colspan="6" class="empty">No grants found.</td></tr>';
    return;
  }

  const filtered = grantsCache.filter((g) => {
    if (!filter) return true;
    const search = `${g.name || ""} ${g.serverRef?.name || ""} ${
      g.subject?.humanID || ""
    } ${g.subject?.agentID || ""}`.toLowerCase();
    return search.includes(filter);
  });

  if (filtered.length === 0) {
    tbody.innerHTML = '<tr><td colspan="6" class="empty">No grants match filter.</td></tr>';
    return;
  }

  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();

  filtered.forEach((grant) => {
    const status = grant.disabled ? "Disabled" : "Active";
    const statusClass = grant.disabled ? "badge-muted" : "badge-success";
    const namespace = grant.namespace || defaults.namespace;
    const serverNamespace = grant.serverRef?.namespace || namespace;

    const row = document.createElement("tr");
    row.appendChild(createIdentityCell(grant.name || "-", namespace));
    row.appendChild(createIdentityCell(grant.serverRef?.name || "-", serverNamespace));
    row.appendChild(createSubjectCell(grant.subject));
    row.appendChild(createTrustCell(grant.maxTrust));
    row.appendChild(createBadgeCell(status, statusClass));
    row.appendChild(
      createActionCell(grant.disabled ? "Enable" : "Disable", () =>
        toggleGrant(grant.namespace || "", grant.name || "", grant.disabled)
      )
    );
    fragment.appendChild(row);
  });

  tbody.appendChild(fragment);
}

async function toggleGrant(namespace, name, currentlyDisabled) {
  const action = currentlyDisabled ? "enable" : "disable";
  const confirmMessage = currentlyDisabled
    ? `Enable grant "${name}"?`
    : `Disable grant "${name}"?`;

  if (!(await confirmModal(confirmMessage))) return;

  try {
    await fetchJSON(
      `/runtime/grants/${encodePathSegment(namespace)}/${encodePathSegment(name)}/${action}`,
      {
      method: "POST",
      }
    );
    showToast(`Grant ${action}d successfully`);
    loadGrants();
    loadDashboardSummary();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    showToast(`Failed to ${action} grant: ${err.message}`, "error");
  }
}

async function applyGrant(event) {
  event.preventDefault();
  const submit = event.submitter;
  if (submit?.disabled) return;

  const name = fieldValue("grant-name");
  const server = fieldValue("grant-server");
  if (!name || !server) {
    showToast("Grant name and server are required.", "error");
    return;
  }
  const humanID = fieldValue("grant-human");
  const agentID = fieldValue("grant-agent");
  if (!humanID && !agentID) {
    showToast("Provide at least one of Human ID or Agent ID.", "error");
    return;
  }

  let toolRules;
  try {
    toolRules = parseToolRules(fieldValue("grant-tool-rules"));
  } catch (parseErr) {
    showToast(parseErr.message, "error");
    return;
  }

  if (submit) submit.disabled = true;
  try {
    const payload = {
      name,
      namespace: fieldValue("grant-namespace") || defaults.namespace,
      serverRef: {
        name: server,
        namespace: fieldValue("grant-server-namespace"),
      },
      subject: { humanID, agentID },
      maxTrust: fieldValue("grant-trust"),
      policyVersion: fieldValue("grant-policy-version") || defaults.policyVersion,
      toolRules,
    };
    await fetchJSON("/runtime/grants", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    showToast(`Grant "${payload.name}" applied successfully`);
    document.getElementById("grant-form")?.reset();
    setFieldValue("grant-namespace", defaults.namespace);
    setFieldValue("grant-policy-version", defaults.policyVersion);
    document.getElementById("grant-form")?.classList.add("hidden");
    loadGrants();
    loadDashboardSummary();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    showToast(`Failed to apply grant: ${err.message}`, "error");
  } finally {
    if (submit) submit.disabled = false;
  }
}

function parseToolRules(text) {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const parts = line.split(":").map((part) => part.trim());
      let decision = parts.pop()?.toLowerCase() || "";
      let requiredTrust = "";
      const trustLevels = new Set(["low", "medium", "high"]);
      const decisions = new Set(["allow", "deny"]);
      if (!decisions.has(decision) && trustLevels.has(decision) && parts.length >= 2) {
        requiredTrust = decision;
        decision = parts.pop()?.toLowerCase() || "";
      }
      const name = parts.join(":").trim();
      if (!name || !decisions.has(decision) || (requiredTrust && !trustLevels.has(requiredTrust))) {
        throw new Error(
          `Invalid tool rule "${line}". Use <name>:allow or <name>:deny or <name>:allow:<trust> (names may include ":"; decision is allow or deny; trust is low, medium, or high).`
        );
      }
      const rule = { name, decision };
      if (requiredTrust) {
        rule.requiredTrust = requiredTrust;
      }
      return rule;
    });
}

// Governance - Sessions
async function loadSessions() {
  try {
    const data = await fetchJSON("/runtime/sessions");
    sessionsCache = Array.isArray(data.sessions) ? data.sessions : [];
    renderGovernanceSummary();
    renderSessions();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load sessions:", err);
    sessionsCache = [];
    renderGovernanceSummary();
    document.getElementById("sessions-body").innerHTML =
      '<tr><td colspan="6" class="empty">Error loading sessions.</td></tr>';
  }
}

function renderSessions() {
  const tbody = document.getElementById("sessions-body");
  if (!tbody) return;
  const filter = document.getElementById("session-filter")?.value.toLowerCase() || "";

  if (sessionsCache.length === 0) {
    tbody.innerHTML = '<tr><td colspan="6" class="empty">No sessions found.</td></tr>';
    return;
  }

  const filtered = sessionsCache.filter((s) => {
    if (!filter) return true;
    const search = `${s.name || ""} ${s.serverRef?.name || ""} ${
      s.subject?.humanID || ""
    } ${s.subject?.agentID || ""}`.toLowerCase();
    return search.includes(filter);
  });

  if (filtered.length === 0) {
    tbody.innerHTML = '<tr><td colspan="6" class="empty">No sessions match filter.</td></tr>';
    return;
  }

  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();

  filtered.forEach((session) => {
    const status = session.revoked ? "Revoked" : "Active";
    const statusClass = session.revoked ? "badge-error" : "badge-success";
    const namespace = session.namespace || defaults.namespace;
    const serverNamespace = session.serverRef?.namespace || namespace;

    const row = document.createElement("tr");
    row.appendChild(createIdentityCell(session.name || "-", namespace));
    row.appendChild(createIdentityCell(session.serverRef?.name || "-", serverNamespace));
    row.appendChild(createSubjectCell(session.subject));
    row.appendChild(createTrustCell(session.consentedTrust));
    row.appendChild(createBadgeCell(status, statusClass));
    row.appendChild(
      createActionCell(session.revoked ? "Unrevoke" : "Revoke", () =>
        toggleSession(
          session.namespace || "",
          session.name || "",
          session.revoked
        )
      )
    );
    fragment.appendChild(row);
  });

  tbody.appendChild(fragment);
}

async function toggleSession(namespace, name, currentlyRevoked) {
  const action = currentlyRevoked ? "unrevoke" : "revoke";
  const confirmMessage = currentlyRevoked
    ? `Unrevoke session "${name}"?`
    : `Revoke session "${name}"?`;

  if (!(await confirmModal(confirmMessage))) return;

  try {
    await fetchJSON(
      `/runtime/sessions/${encodePathSegment(namespace)}/${encodePathSegment(name)}/${action}`,
      {
      method: "POST",
      }
    );
    showToast(`Session ${action}d successfully`);
    loadSessions();
    loadDashboardSummary();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    showToast(`Failed to ${action} session: ${err.message}`, "error");
  }
}

async function applySession(event) {
  event.preventDefault();
  const submit = event.submitter;
  if (submit?.disabled) return;

  const name = fieldValue("session-name");
  const server = fieldValue("session-server");
  if (!name || !server) {
    showToast("Session name and server are required.", "error");
    return;
  }
  const humanID = fieldValue("session-human");
  const agentID = fieldValue("session-agent");
  if (!humanID && !agentID) {
    showToast("Provide at least one of Human ID or Agent ID.", "error");
    return;
  }

  let expiresAt;
  try {
    expiresAt = dateTimeLocalToISOString(fieldValue("session-expires-at"));
  } catch (parseErr) {
    showToast(parseErr.message, "error");
    return;
  }

  if (submit) submit.disabled = true;
  try {
    const payload = {
      name,
      namespace: fieldValue("session-namespace") || defaults.namespace,
      serverRef: {
        name: server,
        namespace: fieldValue("session-server-namespace"),
      },
      subject: { humanID, agentID },
      consentedTrust: fieldValue("session-trust"),
      policyVersion: fieldValue("session-policy-version") || defaults.policyVersion,
    };
    if (expiresAt) {
      payload.expiresAt = expiresAt;
    }

    await fetchJSON("/runtime/sessions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    showToast(`Session "${payload.name}" applied successfully`);
    document.getElementById("session-form")?.reset();
    setFieldValue("session-namespace", defaults.namespace);
    setFieldValue("session-policy-version", defaults.policyVersion);
    document.getElementById("session-form")?.classList.add("hidden");
    loadSessions();
    loadDashboardSummary();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    showToast(`Failed to apply session: ${err.message}`, "error");
  } finally {
    if (submit) submit.disabled = false;
  }
}

function fieldValue(id) {
  return document.getElementById(id)?.value.trim() || "";
}

function setFieldValue(id, value) {
  const input = document.getElementById(id);
  if (input) {
    input.value = value;
  }
}

function dateTimeLocalToISOString(value) {
  if (!value) {
    return "";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    throw new Error("Expires At must be a valid date and time.");
  }
  return date.toISOString();
}

function updateSessionExpiresUTCHint() {
  const hint = document.getElementById("session-expires-utc");
  if (!hint) {
    return;
  }
  const val = fieldValue("session-expires-at");
  if (!val) {
    hint.textContent = "";
    hint.classList.add("hidden");
    return;
  }
  try {
    const iso = dateTimeLocalToISOString(val);
    hint.textContent = `Sent to API as UTC: ${iso}`;
    hint.classList.remove("hidden");
  } catch {
    hint.textContent = "";
    hint.classList.add("hidden");
  }
}

function initGovernance() {
  setFieldValue("grant-namespace", defaults.namespace);
  setFieldValue("grant-policy-version", defaults.policyVersion);
  setFieldValue("session-namespace", defaults.namespace);
  setFieldValue("session-policy-version", defaults.policyVersion);

  document
    .getElementById("session-expires-at")
    ?.addEventListener("input", updateSessionExpiresUTCHint);
  document
    .getElementById("session-expires-at")
    ?.addEventListener("change", updateSessionExpiresUTCHint);

  document.getElementById("refresh-grants")?.addEventListener("click", loadGrants);
  document.getElementById("refresh-sessions")?.addEventListener("click", loadSessions);
  document.getElementById("show-grant-form")?.addEventListener("click", () => {
    document.getElementById("grant-form")?.classList.toggle("hidden");
  });
  document.getElementById("cancel-grant-form")?.addEventListener("click", () => {
    document.getElementById("grant-form")?.classList.add("hidden");
  });
  document.getElementById("grant-form")?.addEventListener("submit", applyGrant);
  document.getElementById("show-session-form")?.addEventListener("click", () => {
    document.getElementById("session-form")?.classList.toggle("hidden");
  });
  document.getElementById("cancel-session-form")?.addEventListener("click", () => {
    document.getElementById("session-form")?.classList.add("hidden");
  });
  document.getElementById("session-form")?.addEventListener("submit", applySession);

  const debouncedRenderGrants = debounce(renderGrants, 80);
  const debouncedRenderSessions = debounce(renderSessions, 80);
  document.getElementById("grant-filter")?.addEventListener("input", debouncedRenderGrants);
  document.getElementById("session-filter")?.addEventListener("input", debouncedRenderSessions);
}

// User API Keys
async function loadUserAPIKeys() {
  clearOneTimeUserAPIKey();
  try {
    const data = await fetchJSON("/user/api-keys");
    userAPIKeysCache = Array.isArray(data.keys) ? data.keys : [];
    renderUserAPIKeys();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load user api keys:", err);
    showToast("Failed to load API keys", "error");
  }
}

function renderUserAPIKeys() {
  const tbody = document.getElementById("user-api-keys-body");
  if (!tbody) return;
  if (!userAPIKeysCache.length) {
    tbody.innerHTML = '<tr><td colspan="5" class="empty">No API keys found.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  userAPIKeysCache.forEach((key) => {
    const row = document.createElement("tr");
    row.appendChild(createTextCell(key.name || "-"));
    row.appendChild(createTextCell(key.prefix || "-"));
    row.appendChild(createTextCell(key.created_at ? new Date(key.created_at).toLocaleString() : "-"));
    row.appendChild(createBadgeCell(key.revoked ? "Revoked" : "Active", key.revoked ? "badge-error" : "badge-success"));
    if (key.revoked) {
      row.appendChild(createTextCell("-"));
    } else {
      row.appendChild(
        createActionCell("Revoke", async () => {
          if (!(await confirmModal(`Revoke API key "${key.name}"?`))) return;
          await revokeUserAPIKey(key.id);
        })
      );
    }
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

async function createUserAPIKey() {
  const input = document.getElementById("user-api-key-name");
  const name = (input?.value || "").trim();
  if (!name) {
    showToast("Enter a name for the API key", "warning");
    return;
  }
  try {
    const data = await fetchJSON("/user/api-keys", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    const oneTime = document.getElementById("user-api-key-once");
    if (oneTime && data.api_key) {
      oneTime.textContent = `Copy now (shown once): ${data.api_key}`;
      oneTime.classList.remove("hidden");
      if (userAPIKeyClearTimer) {
        clearTimeout(userAPIKeyClearTimer);
      }
      userAPIKeyClearTimer = setTimeout(() => {
        clearOneTimeUserAPIKey();
      }, 60000);
    }
    if (input) input.value = "";
    showToast("API key created");
    await loadUserAPIKeys();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    showToast(`Failed to create API key: ${err.message}`, "error");
  }
}

async function revokeUserAPIKey(id) {
  try {
    await fetchJSON(`/user/api-keys/${encodePathSegment(id)}/revoke`, { method: "POST" });
    showToast("API key revoked");
    await loadUserAPIKeys();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    showToast(`Failed to revoke API key: ${err.message}`, "error");
  }
}

function resetUserAPIKeys() {
  userAPIKeysCache = [];
  clearOneTimeUserAPIKey();
  const tbody = document.getElementById("user-api-keys-body");
  if (tbody) {
    tbody.innerHTML = '<tr><td colspan="5" class="empty">No API keys found.</td></tr>';
  }
}

function clearOneTimeUserAPIKey() {
  if (userAPIKeyClearTimer) {
    clearTimeout(userAPIKeyClearTimer);
    userAPIKeyClearTimer = null;
  }
  const once = document.getElementById("user-api-key-once");
  if (!once) return;
  once.textContent = "";
  once.classList.add("hidden");
}

function initUserAPIKeys() {
  document.getElementById("refresh-user-api-keys")?.addEventListener("click", loadUserAPIKeys);
  document.getElementById("create-user-api-key")?.addEventListener("click", createUserAPIKey);
}

// Operations - Components
async function loadComponents() {
  const grid = document.getElementById("components-grid");
  grid.innerHTML = '<div class="component-card loading">Loading components...</div>';

  try {
    const data = await fetchJSON("/runtime/components");

    if (!data.components || data.components.length === 0) {
      grid.innerHTML =
        '<div class="component-card loading">No components found.</div>';
      return;
    }

    grid.innerHTML = "";
    const fragment = document.createDocumentFragment();

    data.components.forEach((comp) => {
      const statusClass =
        comp.status === "Ready"
          ? "status-ready"
          : comp.status === "Degraded"
          ? "status-degraded"
          : comp.status === "NotReady"
          ? "status-notready"
          : "";

      const card = document.createElement("div");
      card.className = `component-card ${statusClass}`;
      card.innerHTML = `
        <div class="component-name">${escapeHtml(comp.display)}</div>
        <div class="component-status">${escapeHtml(comp.status)}</div>
        <div class="component-ready">${escapeHtml(comp.ready)}</div>
        ${comp.message ? `<div style="font-size: 11px; color: var(--muted); margin-top: 4px;">${escapeHtml(comp.message)}</div>` : ""}
      `;
      fragment.appendChild(card);
    });

    grid.appendChild(fragment);
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load components:", err);
    grid.innerHTML =
      '<div class="component-card loading">Error loading components.</div>';
  }
}

// Operations - Restart
async function restartComponent() {
  const select = document.getElementById("restart-component-select");
  const component = select.value;

  if (!component) {
    showToast("Please select a component", "warning");
    return;
  }

  if (
    !(await confirmModal(`Restart the "${component}" component?`))
  ) {
    return;
  }

  try {
    await fetchJSON("/runtime/actions/restart", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ component }),
    });
    showToast(`Component "${component}" restart initiated`);
    select.value = "";
    setTimeout(loadComponents, 3000);
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    showToast(`Failed to restart component: ${err.message}`, "error");
  }
}

function initOperations() {
  document.getElementById("refresh-components")?.addEventListener("click", loadComponents);
  document.getElementById("restart-component-btn")?.addEventListener("click", restartComponent);
}

// Modal
let modalResolve = null;

function initModal() {
  document.getElementById("modal-cancel")?.addEventListener("click", () => {
    document.getElementById("modal").classList.add("hidden");
    if (modalResolve) {
      modalResolve(false);
      modalResolve = null;
    }
  });

  document.getElementById("modal-confirm")?.addEventListener("click", () => {
    document.getElementById("modal").classList.add("hidden");
    if (modalResolve) {
      modalResolve(true);
      modalResolve = null;
    }
  });
}

function confirmModal(message) {
  return new Promise((resolve) => {
    modalResolve = resolve;
    document.getElementById("modal-message").textContent = message;
    document.getElementById("modal").classList.remove("hidden");
  });
}

// Initialize
document.addEventListener("DOMContentLoaded", () => {
  initTabs();
  initDashboard();
  initGovernance();
  initUserAPIKeys();
  initOperations();
  initModal();
  initAuth();
});
