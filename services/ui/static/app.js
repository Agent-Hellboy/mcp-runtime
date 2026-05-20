const apiBase = window.MCP_API_BASE || "/api";
const defaults = Object.assign(
  { namespace: "", policyVersion: "v1" },
  window.MCP_DEFAULTS || {}
);
const platformMode = window.MCP_PLATFORM_MODE || "tenant";
const publicCatalogEnabled = platformMode === "public";
let authenticated = null;
let authPrincipal = null;
let grantsCache = [];
let sessionsCache = [];
let userAPIKeysCache = [];
let teamsCache = [];
let teamMembersCache = [];
let serversCache = [];
let publishPolicyCache = null;
let selectedServerKey = "";
let selectedServerEventsCache = [];
let userDashboardServersCache = [];
let userDashboardAnalyticsCache = null;
let operationsServersCache = [];
let operationsEventsCache = [];
let operationsAuditCache = [];
let operationsUsersCache = [];
let operationsImagesCache = [];
let operationsDeploymentsCache = [];
let userAPIKeyClearTimer = null;
let serverSearchQuery = "";
let serverStatusFilter = "all";
let selectedOperationsServerKey = "";
let selectedUserAnalyticsServerKey = "";
let selectedTeamSlug = "";
let namespaceScopes = [];
let selectedNamespace = defaults.namespace || "";

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

async function fetchJSONNoAuthSideEffects(path, options = {}) {
  const headers = { ...options.headers };

  const response = await fetch(`${apiBase}${path}`, {
    ...options,
    credentials: "same-origin",
    headers,
  });

  if (!response.ok) {
    const error = await response.text();
    const err = new Error(error || `Request failed: ${response.status}`);
    err.status = response.status;
    throw err;
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

function readErrorMessage(err, fallback) {
  const message = String(err?.message || "").trim();
  if (!message) return fallback;
  try {
    const parsed = JSON.parse(message);
    if (parsed?.error) return parsed.error;
  } catch (_) {
    // Use the plain error text below.
  }
  return message || fallback;
}

function activeScopeNamespace() {
  if (!authenticated && !publicCatalogEnabled) return "";
  if (selectedNamespace !== undefined && selectedNamespace !== null) {
    return String(selectedNamespace).trim();
  }
  return (defaults.namespace || "").trim();
}

function scopedPath(path) {
  const namespace = activeScopeNamespace();
  if (!namespace) return path;
  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}namespace=${encodeURIComponent(namespace)}`;
}

function namespaceScopeLabel(item) {
  if (item?.is_admin_fleet || item?.scope === "all") {
    return "all namespaces";
  }
  if (item?.is_public || item?.scope === "public") {
    return `public / ${item.namespace}`;
  }
  if (item?.scope === "org") {
    return `org / ${item.namespace}`;
  }
  if (item?.is_catalog) {
    return platformMode === "tenant" ? "tenant namespaces" : "org + teams";
  }
  if (item?.is_shared) {
    return `org / ${item.namespace}`;
  }
  if (item?.team_slug) {
    return `team:${item.team_slug} / ${item.namespace}`;
  }
  return item?.namespace || "-";
}

function syncScopeSelector() {
  const select = document.getElementById("scope-namespace");
  if (!select) return;
  select.innerHTML = "";
  const scopes = Array.isArray(namespaceScopes) ? namespaceScopes : [];
  if (scopes.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = authenticated ? "No namespaces" : "Sign in required";
    select.appendChild(option);
    select.disabled = true;
    selectedNamespace = "";
    setFieldValue("grant-namespace", "");
    setFieldValue("session-namespace", "");
    return;
  }
  scopes.forEach((item) => {
    const option = document.createElement("option");
    option.value = item.namespace || "";
    option.textContent = namespaceScopeLabel(item);
    select.appendChild(option);
  });
  select.disabled = false;
  const hasSelected = scopes.some((item) => (item.namespace || "") === selectedNamespace);
  if (!hasSelected) {
    selectedNamespace = scopes[0]?.namespace || "";
  }
  select.value = selectedNamespace;
  setFieldValue("grant-namespace", activeScopeNamespace());
  setFieldValue("session-namespace", activeScopeNamespace());
}

function ensureNamespaceScope(namespace) {
  const normalized = String(namespace || "").trim();
  if (!normalized) return;
  if (namespaceScopes.some((item) => String(item?.namespace || "").trim() === normalized)) return;
  namespaceScopes = [...namespaceScopes, { namespace: normalized, scope: "namespace" }];
}

function focusNamespaceScope(namespace) {
  const normalized = String(namespace || "").trim();
  if (!normalized) return;
  ensureNamespaceScope(normalized);
  selectedNamespace = normalized;
  syncScopeSelector();
}

function publicPreviewScopes() {
  return [{
    namespace: defaults.namespace || "mcp-servers-public",
    scope: "public",
    scope_name: "Public preview",
    is_public: true,
  }];
}

function adminAllNamespaceScope() {
  return {
    namespace: "",
    scope: "all",
    scope_name: "All namespaces",
    is_admin_fleet: true,
  };
}

async function loadNamespaceScopes() {
  if (!authenticated) {
    namespaceScopes = publicCatalogEnabled ? publicPreviewScopes() : [];
    selectedNamespace = publicCatalogEnabled ? namespaceScopes[0]?.namespace || "" : "";
    syncScopeSelector();
    return;
  }
  try {
    const data = await fetchJSON("/runtime/namespaces");
    namespaceScopes = Array.isArray(data.namespaces) ? data.namespaces.filter((item) => item?.namespace) : [];
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load namespaces:", err);
    namespaceScopes = [];
  }
  if (authPrincipal?.role === "admin" && namespaceScopes.length > 1) {
    namespaceScopes = [adminAllNamespaceScope(), ...namespaceScopes];
  } else if (authPrincipal?.role !== "admin" && namespaceScopes.length > 1) {
    namespaceScopes = [{ namespace: "", scope: "catalog", is_catalog: true }, ...namespaceScopes];
  }
  if (namespaceScopes.some((item) => item.is_admin_fleet) && (!selectedNamespace || selectedNamespace === defaults.namespace)) {
    selectedNamespace = "";
  }
  if (namespaceScopes.some((item) => item.is_catalog) && (!selectedNamespace || selectedNamespace === defaults.namespace)) {
    selectedNamespace = "";
  }
  syncScopeSelector();
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

function setInlineError(id, message = "") {
  const node = document.getElementById(id);
  if (!node) return;
  const text = String(message || "").trim();
  node.textContent = text;
  node.classList.toggle("hidden", !text);
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
      } else if (target === "userdashboard") {
        loadUserDashboard();
      } else if (target === "governance") {
        loadGrants();
        loadSessions();
      } else if (target === "teams") {
        loadTeams();
      } else if (target === "operations") {
        loadMCPOperations();
      } else if (target === "platform") {
        loadPlatformManagement();
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

function createCodeCell(text) {
  const cell = document.createElement("td");
  const code = document.createElement("code");
  code.className = "table-code";
  code.textContent = text || "-";
  cell.appendChild(code);
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
  if (subject.teamID) {
    chips.appendChild(createSubjectChip("Team", subject.teamID));
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

function createGrantRiskCell(trust, sideEffects) {
  const cell = document.createElement("td");
  const stack = document.createElement("div");
  stack.className = "chip-stack";
  stack.appendChild(createBadge(trust || "-", trustBadgeClass(trust || "-")));
  const effects = Array.isArray(sideEffects) && sideEffects.length ? sideEffects : ["none"];
  effects.forEach((effect) => stack.appendChild(createBadge(effect, "badge-muted")));
  cell.appendChild(stack);
  return cell;
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
    await loadNamespaceScopes();
    loadActiveTab();
    startAutoRefresh();
  } else {
    await loadNamespaceScopes();
    activateTab("servers");
    if (publicCatalogEnabled) {
      loadServers();
    } else {
      renderSignedOutServerCatalog();
    }
  }
}

async function handleAuthSubmit(event) {
  event.preventDefault();
  const apiKeyInput = document.getElementById("api-key-input");
  const emailInput = document.getElementById("auth-email-input");
  const passwordInput = document.getElementById("auth-password-input");
  const submit = document.getElementById("auth-submit");
  const apiKey = apiKeyInput?.value.trim() || "";
  const email = emailInput?.value.trim() || "";
  const password = passwordInput?.value || "";
  const hasAPIKey = apiKey !== "";
  const hasEmail = email !== "";
  const hasPassword = password !== "";

  setAuthError("");
  if (!hasAPIKey && ((hasEmail && !hasPassword) || (!hasEmail && hasPassword))) {
    setAuthError("Enter both email and password, or use an API key.");
    return;
  }
  if (!hasAPIKey && !hasEmail && !hasPassword) {
    setAuthError("Provide email and password, an API key, or sign in with Google.");
    return;
  }
  if (submit) submit.disabled = true;
  try {
    const payload = hasAPIKey ? { api_key: apiKey } : { email, password };
    const data = await performLogin(payload);
    authPrincipal = data?.principal || null;
    if (apiKeyInput) apiKeyInput.value = "";
    if (passwordInput) passwordInput.value = "";
    hideAuthModal();
    setAuthenticated(true);
    await loadNamespaceScopes();
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
    await loadNamespaceScopes();
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
  namespaceScopes = publicCatalogEnabled ? publicPreviewScopes() : [];
  selectedNamespace = namespaceScopes[0]?.namespace || "";
  syncScopeSelector();
  resetDashboard();
  resetUserDashboard();
  resetGovernance();
  resetTeams();
  resetUserAPIKeys();
  activateTab("servers");
  if (publicCatalogEnabled) {
    loadServers();
  } else {
    renderSignedOutServerCatalog();
  }
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
  } else if (active === "userdashboard") {
    loadUserDashboard();
  } else if (active === "servers") {
    loadServers();
  } else if (active === "governance") {
    loadGrants();
    loadSessions();
  } else if (active === "teams") {
    loadTeams();
  } else if (active === "operations") {
    loadMCPOperations();
  } else if (active === "platform") {
    loadPlatformManagement();
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

function resetUserDashboard() {
  userDashboardServersCache = [];
  userDashboardAnalyticsCache = null;
  setText("userdash-server-total", "-");
  setText("userdash-ready-total", "-");
  setText("userdash-events", "-");
  setText("userdash-denied", "-");
  setText("userdash-error-rate", "-");
  const serverBody = document.getElementById("user-dashboard-servers-body");
  const breakdownBody = document.getElementById("user-analytics-breakdown-body");
  const toolsBody = document.getElementById("user-analytics-tools-body");
  const recentBody = document.getElementById("user-analytics-recent-body");
  if (serverBody) serverBody.innerHTML = '<tr><td colspan="5" class="empty">No MCP servers found.</td></tr>';
  if (breakdownBody) breakdownBody.innerHTML = '<tr><td colspan="6" class="empty">No usage yet.</td></tr>';
  if (toolsBody) toolsBody.innerHTML = '<tr><td colspan="4" class="empty">No tool calls yet.</td></tr>';
  if (recentBody) recentBody.innerHTML = '<tr><td colspan="4" class="empty">No recent activity.</td></tr>';
}

async function loadUserDashboard() {
  if (!authenticated) {
    resetUserDashboard();
    showAuthModal();
    return;
  }
  await Promise.allSettled([loadUserDashboardServers(), loadUserDashboardAnalytics()]);
  renderUserDashboardSummary();
}

async function loadUserDashboardServers() {
  const tbody = document.getElementById("user-dashboard-servers-body");
  if (tbody) {
    tbody.innerHTML = '<tr><td colspan="5" class="empty">Loading MCP servers...</td></tr>';
  }
  try {
    const data = authPrincipal?.role === "admin"
      ? { servers: await loadFleetServers() }
      : await fetchJSON("/runtime/servers");
    userDashboardServersCache = filterUserDashboardServers(Array.isArray(data.servers) ? data.servers : []);
    syncUserAnalyticsServerSelect();
    renderUserDashboardServers();
    renderUserDashboardSummary();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load user dashboard servers:", err);
    userDashboardServersCache = [];
    syncUserAnalyticsServerSelect();
    renderUserDashboardSummary();
    if (tbody) {
      tbody.innerHTML = '<tr><td colspan="5" class="empty">Error loading MCP servers.</td></tr>';
    }
  }
}

async function loadUserDashboardAnalytics() {
  const breakdownBody = document.getElementById("user-analytics-breakdown-body");
  if (breakdownBody) {
    breakdownBody.innerHTML = '<tr><td colspan="6" class="empty">Loading usage...</td></tr>';
  }
  try {
    const data = await fetchJSON(`/user/analytics/usage?${userAnalyticsQuery()}`);
    userDashboardAnalyticsCache = data || null;
    renderUserDashboardAnalytics(data);
    renderUserDashboardSummary();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load user analytics:", err);
    userDashboardAnalyticsCache = null;
    renderUserDashboardSummary();
    renderUserAnalyticsError();
  }
}

function userAnalyticsQuery() {
  const params = new URLSearchParams();
  params.set("limit", "10");
  params.set("window_days", document.getElementById("user-analytics-window")?.value || "7");
  const selected = userAnalyticsSelectedServer();
  if (selected.namespace) params.set("namespace", selected.namespace);
  if (selected.name) params.set("server", selected.name);
  return params.toString();
}

function userAnalyticsSelectedServer() {
  const key = selectedUserAnalyticsServerKey || "";
  const idx = key.indexOf("/");
  if (idx < 0) {
    return { namespace: "", name: "" };
  }
  return {
    namespace: key.slice(0, idx),
    name: key.slice(idx + 1),
  };
}

function syncUserAnalyticsServerSelect() {
  const select = document.getElementById("user-analytics-server");
  if (!select) return;
  const previous = selectedUserAnalyticsServerKey;
  select.innerHTML = '<option value="">All servers</option>';
  userDashboardServersCache.forEach((server) => {
    const option = document.createElement("option");
    option.value = operationServerKey(server);
    option.textContent = `${server.namespace || "-"} / ${server.name || "-"}`;
    select.appendChild(option);
  });
  const exists = userDashboardServersCache.some((server) => operationServerKey(server) === previous);
  selectedUserAnalyticsServerKey = exists ? previous : "";
  select.value = selectedUserAnalyticsServerKey;
}

function filterUserDashboardServers(servers) {
  if (authPrincipal?.role === "admin") {
    return servers;
  }
  return servers.filter((server) => !isSharedCatalogNamespace(server?.namespace));
}

function isSharedCatalogNamespace(namespace) {
  const normalized = String(namespace || "").trim();
  if (!normalized) return false;
  return normalized === "mcp-servers" || namespaceScopes.some((scope) =>
    String(scope?.namespace || "").trim() === normalized && scope?.is_shared
  );
}

function renderUserDashboardSummary() {
  const total = userDashboardServersCache.length;
  const ready = userDashboardServersCache.filter((server) => server.status === "Ready").length;
  const totals = userDashboardAnalyticsCache?.totals || {};
  const events = Number(totals.events || 0);
  const denied = Number(totals.denied || 0);
  setText("userdash-server-total", formatNumber(total));
  setText("userdash-ready-total", formatNumber(ready));
  setText("userdash-events", formatNumber(events));
  setText("userdash-denied", formatNumber(denied));
  setText("userdash-error-rate", events > 0 ? `${Math.round((denied / events) * 100)}%` : "-");
}

function renderUserDashboardServers() {
  const tbody = document.getElementById("user-dashboard-servers-body");
  if (!tbody) return;
  if (!userDashboardServersCache.length) {
    tbody.innerHTML = '<tr><td colspan="5" class="empty">No MCP servers found.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  userDashboardServersCache.forEach((server) => {
    const row = document.createElement("tr");
    row.appendChild(createIdentityCell(server.name || "-", server.namespace || "-"));
    row.appendChild(createBadgeCell(server.status || "Unknown", serverBadgeClass(server.status)));
    row.appendChild(createTextCell(operationInventoryLabel(server)));
    row.appendChild(createEndpointCell(server.endpoint));
    row.appendChild(createUserServerActionsCell(server));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function createUserServerActionsCell(server) {
  const cell = document.createElement("td");
  const actions = document.createElement("div");
  actions.className = "table-action-row";

  const analyticsButton = document.createElement("button");
  analyticsButton.type = "button";
  analyticsButton.className = "ghost action-btn";
  analyticsButton.textContent = "Analytics";
  analyticsButton.addEventListener("click", () => {
    selectedUserAnalyticsServerKey = operationServerKey(server);
    const select = document.getElementById("user-analytics-server");
    if (select) select.value = selectedUserAnalyticsServerKey;
    loadUserDashboardAnalytics();
  });
  actions.appendChild(analyticsButton);

  if (server.endpoint) {
    const copyButton = document.createElement("button");
    copyButton.type = "button";
    copyButton.className = "ghost action-btn";
    copyButton.textContent = "Copy URL";
    copyButton.addEventListener("click", () => copyTextToClipboard(server.endpoint, "Endpoint copied"));
    actions.appendChild(copyButton);
  }

  if (isTenantUser() && server.namespace && server.name) {
    const retireButton = document.createElement("button");
    retireButton.type = "button";
    retireButton.className = "ghost danger action-btn";
    retireButton.textContent = "Retire";
    retireButton.addEventListener("click", () => retireServer(server));
    actions.appendChild(retireButton);
  }

  cell.appendChild(actions);
  return cell;
}

function renderUserDashboardAnalytics(data) {
  renderUserAnalyticsBreakdown(data?.servers || []);
  renderUserAnalyticsTools(data?.tools || []);
  renderUserAnalyticsRecent(data?.recent || []);
}

function renderUserAnalyticsBreakdown(rows) {
  const tbody = document.getElementById("user-analytics-breakdown-body");
  if (!tbody) return;
  if (!rows.length) {
    tbody.innerHTML = '<tr><td colspan="6" class="empty">No usage yet.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  rows.forEach((item) => {
    const row = document.createElement("tr");
    row.appendChild(createIdentityCell(item.server || "-", item.namespace || "-"));
    row.appendChild(createTextCell(formatNumber(item.events || 0)));
    row.appendChild(createTextCell(formatNumber(item.allowed || 0)));
    row.appendChild(createTextCell(formatNumber(item.denied || 0)));
    row.appendChild(createTextCell(formatNumber(item.unique_humans || 0)));
    row.appendChild(createTextCell(formatNumber(item.unique_agents || 0)));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function renderUserAnalyticsTools(rows) {
  const tbody = document.getElementById("user-analytics-tools-body");
  if (!tbody) return;
  if (!rows.length) {
    tbody.innerHTML = '<tr><td colspan="4" class="empty">No tool calls yet.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  rows.forEach((item) => {
    const row = document.createElement("tr");
    row.appendChild(createIdentityCell(item.server || "-", ""));
    row.appendChild(createTextCell(item.tool_name || "-"));
    row.appendChild(createTextCell(formatNumber(item.events || 0)));
    row.appendChild(createTextCell(formatNumber(item.denied || 0)));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function renderUserAnalyticsRecent(rows) {
  const tbody = document.getElementById("user-analytics-recent-body");
  if (!tbody) return;
  if (!rows.length) {
    tbody.innerHTML = '<tr><td colspan="4" class="empty">No recent activity.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  rows.forEach((item) => {
    const row = document.createElement("tr");
    row.appendChild(createTextCell(formatDateTime(item.timestamp)));
    row.appendChild(createIdentityCell(item.server || "-", item.namespace || ""));
    row.appendChild(createTextCell(item.tool_name || item.event_type || "-"));
    row.appendChild(createBadgeCell(item.decision || "unknown", decisionBadgeClass(item.decision)));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function renderUserAnalyticsError() {
  const breakdownBody = document.getElementById("user-analytics-breakdown-body");
  const toolsBody = document.getElementById("user-analytics-tools-body");
  const recentBody = document.getElementById("user-analytics-recent-body");
  if (breakdownBody) breakdownBody.innerHTML = '<tr><td colspan="6" class="empty">Error loading usage.</td></tr>';
  if (toolsBody) toolsBody.innerHTML = '<tr><td colspan="4" class="empty">Error loading tools.</td></tr>';
  if (recentBody) recentBody.innerHTML = '<tr><td colspan="4" class="empty">Error loading recent activity.</td></tr>';
}

function resetGovernance() {
  grantsCache = [];
  sessionsCache = [];
  setInlineError("grant-form-error");
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
  if (!authenticated && !publicCatalogEnabled) {
    renderSignedOutServerCatalog();
    return;
  }
  try {
    const data = await fetchJSON(scopedPath("/runtime/servers"));
    serversCache = Array.isArray(data.servers) ? data.servers : [];
    publishPolicyCache = data.publish_policy || null;
    renderServers();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load servers:", err);
    serversCache = [];
    publishPolicyCache = null;
    const grid = document.getElementById("servers-grid");
    if (grid) {
      grid.innerHTML = '<div class="component-card error">Error loading MCP servers.</div>';
    }
  }
}

function renderSignedOutServerCatalog() {
  serversCache = [];
  publishPolicyCache = null;
  selectedServerKey = "";
  selectedServerEventsCache = [];
  renderServerCatalogSummary();
  renderServerDetailPanel(null);
  const grid = document.getElementById("servers-grid");
  if (grid) {
    grid.innerHTML = '<div class="server-empty-state">No public catalog is available. Sign in to view organization and private servers.</div>';
  }
}

function renderServers() {
  const grid = document.getElementById("servers-grid");
  if (!grid) return;
  renderServerCatalogSummary();

  if (serversCache.length === 0) {
    grid.innerHTML = '<div class="server-empty-state">No MCP servers found.</div>';
    renderServerDetailPanel(null);
    return;
  }

  const servers = filteredServers();
  if (servers.length === 0) {
    grid.innerHTML = '<div class="server-empty-state">No servers match this search.</div>';
    renderServerDetailPanel(null);
    return;
  }

  grid.innerHTML = "";
  const fragment = document.createDocumentFragment();
  servers.forEach((server) => {
    const card = document.createElement("article");
    card.className = "server-card";
    card.dataset.serverKey = serverKey(server);
    card.tabIndex = 0;
    card.classList.toggle("selected", selectedServerKey === serverKey(server));
    card.addEventListener("click", (event) => {
      if (event.target.closest("button, a, details, summary, code")) return;
      selectServer(server);
    });
    card.addEventListener("keydown", (event) => {
      if (event.key !== "Enter" && event.key !== " ") return;
      event.preventDefault();
      selectServer(server);
    });

    card.appendChild(renderServerHero(server));
    card.appendChild(renderServerMeta(server));
    card.appendChild(renderServerEndpoint(server));

    const displayInventory = serverDisplayInventory(server);
    const inventory = document.createElement("div");
    inventory.className = "server-inventory-grid";
    inventory.appendChild(renderInventoryBlock("Tools", displayInventory.tools, renderToolItem));
    inventory.appendChild(renderInventoryBlock("Prompts", displayInventory.prompts, renderInventoryItem));
    inventory.appendChild(renderInventoryBlock("Resources", displayInventory.resources, renderInventoryItem));
    inventory.appendChild(renderInventoryBlock("Tasks", displayInventory.tasks, renderInventoryItem));
    card.appendChild(inventory);

    if (server.access_json && Object.keys(server.access_json).length) {
      card.appendChild(renderServerConnectConfig(server));
    }

    fragment.appendChild(card);
  });
  grid.appendChild(fragment);
  const selected = serversCache.find((server) => serverKey(server) === selectedServerKey);
  renderServerDetailPanel(selected || null);
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
  const inventory = serverDisplayInventory(server);
  const values = [
    server.name,
    server.namespace,
    server.description,
    server.status,
    server.ready,
    server.endpoint,
    metadataSearchText(server.labels),
    ...(inventory.tools || []).map((tool) => `${tool.name || ""} ${tool.requiredTrust || ""} ${tool.sideEffect || ""} ${tool.description || ""} ${tool.drift || ""}`),
    ...(inventory.prompts || []).map(inventorySearchText),
    ...(inventory.resources || []).map(inventorySearchText),
    ...(inventory.tasks || []).map(inventorySearchText),
  ];
  return values.filter(Boolean).join(" ").toLowerCase();
}

function serverDisplayInventory(server) {
  const declaredTools = Array.isArray(server?.tools) ? server.tools : [];
  const declaredPrompts = Array.isArray(server?.prompts) ? server.prompts : [];
  const declaredResources = Array.isArray(server?.resources) ? server.resources : [];
  const declaredTasks = Array.isArray(server?.tasks) ? server.tasks : [];
  const live = server?.liveInventory;
  if (!live || typeof live !== "object") {
    return {
      tools: declaredTools,
      prompts: declaredPrompts,
      resources: declaredResources,
      tasks: declaredTasks,
    };
  }
  return {
    tools: mergeToolInventory(live.tools || [], declaredTools),
    prompts: mergeNamedInventory(live.prompts || [], declaredPrompts, inventoryNameKey),
    resources: mergeNamedInventory(live.resources || [], declaredResources, resourceInventoryKey),
    tasks: declaredTasks,
  };
}

function mergeToolInventory(liveItems, declaredItems) {
  const declared = mapInventoryByKey(declaredItems, inventoryNameKey);
  const seen = new Set();
  const out = [];
  liveItems.forEach((item) => {
    const key = inventoryNameKey(item);
    if (!key) return;
    const governance = declared.get(key);
    seen.add(key);
    out.push({
      ...item,
      name: item.name || key,
      description: item.description || governance?.description || "",
      requiredTrust: governance?.requiredTrust || "",
      sideEffect: governance?.sideEffect || "",
      labels: governance?.labels || item.labels || {},
      drift: governance ? "" : "ungoverned",
    });
  });
  declaredItems.forEach((item) => {
    const key = inventoryNameKey(item);
    if (!key || seen.has(key)) return;
    out.push({ ...item, drift: "missing" });
  });
  return out;
}

function mergeNamedInventory(liveItems, declaredItems, keyFn) {
  const declared = mapInventoryByKey(declaredItems, keyFn);
  const seen = new Set();
  const out = [];
  liveItems.forEach((item) => {
    const key = keyFn(item);
    if (!key) return;
    const declaredItem = declared.get(key);
    seen.add(key);
    out.push({
      ...item,
      name: item.name || item.uri || key,
      description: item.description || declaredItem?.description || "",
      labels: declaredItem?.labels || item.labels || {},
      drift: declaredItem ? "" : "ungoverned",
    });
  });
  declaredItems.forEach((item) => {
    const key = keyFn(item);
    if (!key || seen.has(key)) return;
    out.push({ ...item, drift: "missing" });
  });
  return out;
}

function mapInventoryByKey(items, keyFn) {
  const out = new Map();
  (Array.isArray(items) ? items : []).forEach((item) => {
    const key = keyFn(item);
    if (key && !out.has(key)) out.set(key, item);
  });
  return out;
}

function inventoryNameKey(item) {
  if (typeof item === "string") return item.trim();
  return String(item?.name || "").trim();
}

function resourceInventoryKey(item) {
  if (typeof item === "string") return item.trim();
  return String(item?.uri || item?.name || "").trim();
}

function metadataSearchText(labels) {
  if (!labels || typeof labels !== "object") return "";
  return Object.entries(labels)
    .map(([key, value]) => `${key} ${value}`)
    .join(" ");
}

function inventorySearchText(item) {
  if (typeof item === "string") return item;
  const labels = metadataSearchText(item?.labels);
  return `${item?.name || ""} ${item?.uri || ""} ${item?.description || ""} ${item?.drift || ""} ${labels}`;
}

function renderServerCatalogSummary() {
  const total = serversCache.length;
  const ready = serversCache.filter((server) => server.status === "Ready").length;
  const tools = serversCache.reduce((sum, server) => sum + serverDisplayInventory(server).tools.length, 0);
  setText("server-count-total", formatNumber(total));
  setText("server-count-ready", formatNumber(ready));
  setText("server-count-tools", formatNumber(tools));
  setText("server-count-quota", formatPublishQuota());
}

function formatPublishQuota() {
  if (!authenticated) {
    return "-";
  }
  if (!publishPolicyCache || publishPolicyCache.active_server_limit_enabled !== true) {
    return "off";
  }
  const count = Number(publishPolicyCache.active_server_count || 0);
  const limit = Number(publishPolicyCache.active_server_limit || 0);
  if (!limit) return "off";
  return `${formatNumber(count)}/${formatNumber(limit)}`;
}

function serverKey(server) {
  return `${server?.namespace || ""}/${server?.name || ""}`;
}

function selectServer(server) {
  selectedServerKey = serverKey(server);
  selectedServerEventsCache = [];
  renderServers();
  loadSelectedServerEvents(server);
}

async function loadSelectedServerEvents(server) {
  if (!authenticated || !server?.namespace || !server?.name) {
    return;
  }
  try {
    const query = new URLSearchParams({
      namespace: server.namespace,
      server: server.name,
      limit: "20",
    });
    const data = await fetchJSONNoAuthSideEffects(`/runtime/server-events?${query.toString()}`);
    if (selectedServerKey !== serverKey(server)) return;
    selectedServerEventsCache = Array.isArray(data.events) ? data.events : [];
    renderServerDetailPanel(server);
  } catch (err) {
    if (selectedServerKey !== serverKey(server)) return;
    selectedServerEventsCache = [];
    if (err?.status === 401 || err?.status === 403) {
      renderServerDetailPanel(server, "Recent activity unavailable.");
      return;
    }
    console.error("Failed to load server events:", err);
    renderServerDetailPanel(server, "Analytics unavailable.");
  }
}

async function retireServer(server) {
  if (!server?.namespace || !server?.name) return;
  const ok = await confirmModal(`Retire ${server.namespace}/${server.name}?`);
  if (!ok) return;
  try {
    await fetchJSON(
      `/runtime/servers/${encodePathSegment(server.namespace)}/${encodePathSegment(server.name)}`,
      { method: "DELETE" }
    );
    showToast("Server retired");
    if (selectedServerKey === serverKey(server)) {
      selectedServerKey = "";
      selectedServerEventsCache = [];
      renderServerDetailPanel(null);
    }
    await loadServers();
    if (isTenantUser()) {
      await loadUserDashboard();
    }
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to retire server:", err);
    showToast(readErrorMessage(err, "Retire failed"), "error");
  }
}

function renderServerDetailPanel(server, errorMessage = "") {
  const panel = document.getElementById("server-detail-panel");
  if (!panel) return;
  if (!server) {
    panel.classList.add("hidden");
    panel.innerHTML = "";
    return;
  }
  panel.classList.remove("hidden");
  const labels = server.labels && typeof server.labels === "object"
    ? Object.entries(server.labels)
    : [];
  const inventory = serverDisplayInventory(server);
  const description = server.description
    ? `<p class="server-description">${escapeHtml(server.description)}</p>`
    : "";
  panel.innerHTML = `
    <div class="server-inspector-head">
      <div class="server-identity">
        <span class="server-avatar" aria-hidden="true">${escapeHtml(serverInitials(server.name))}</span>
        <div class="server-title-stack">
          <h3>${escapeHtml(server.name || "-")}</h3>
          <p>${escapeHtml(server.namespace || "-")}</p>
          ${description}
        </div>
      </div>
      <div class="server-card-actions">
        ${server.endpoint ? '<button class="ghost server-action" id="selected-server-copy-url" type="button">Copy URL</button>' : ""}
        ${authenticated && !isTenantUser() ? '<button class="ghost danger server-action" id="selected-server-retire" type="button">Retire</button>' : ""}
      </div>
    </div>
    <div class="server-detail-grid">
      ${serverDetailStat("Ready Pods", server.ready || "0/0")}
      ${serverDetailStat("Deployed", formatDateTime(server.age))}
      ${serverDetailStat("Tools", String(inventory.tools.length))}
      ${serverDetailStat("Prompts", String(inventory.prompts.length))}
      ${serverDetailStat("Resources", String(inventory.resources.length))}
      ${serverDetailStat("Tasks", String(inventory.tasks.length))}
    </div>
    <div class="server-detail-block">
      <span class="server-detail-label">Endpoint</span>
      <code class="server-endpoint">${escapeHtml(server.endpoint || "No public endpoint")}</code>
    </div>
    <div class="server-detail-block">
      <span class="server-detail-label">Labels</span>
      <div class="server-label-list">
        ${
          labels.length
            ? labels
                .map(([key, value]) => `<span>${escapeHtml(key)}=${escapeHtml(value)}</span>`)
                .join("")
            : '<span class="muted-text">None</span>'
        }
      </div>
    </div>
    <div class="server-detail-block">
      <span class="server-detail-label">Recent Activity</span>
      ${renderSelectedServerEvents(errorMessage)}
    </div>
  `;
  document.getElementById("selected-server-copy-url")?.addEventListener("click", () => {
    copyTextToClipboard(server.endpoint, "Endpoint copied");
  });
  document.getElementById("selected-server-retire")?.addEventListener("click", () => {
    retireServer(server);
  });
}

function renderSelectedServerEvents(errorMessage = "") {
  if (errorMessage) {
    return `<div class="empty">${escapeHtml(errorMessage)}</div>`;
  }
  if (!authenticated) {
    return '<div class="empty">Sign in to view server analytics.</div>';
  }
  if (!selectedServerEventsCache.length) {
    return '<div class="empty">No recent analytics events for this server.</div>';
  }
  const rows = selectedServerEventsCache
    .map((event) => `
      <tr>
        <td>${escapeHtml(formatDateTime(event.timestamp))}</td>
        <td>${escapeHtml(event.event_type || "-")}</td>
        <td>${escapeHtml(event.tool_name || "-")}</td>
        <td>${renderDecision(event.decision)}</td>
      </tr>
    `)
    .join("");
  return `
    <div class="table-wrap compact">
      <table>
        <thead>
          <tr><th>Time</th><th>Event</th><th>Tool</th><th>Decision</th></tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
  `;
}

function renderServerHero(server) {
  const hero = document.createElement("div");
  hero.className = "server-card-hero";
  const description = server.description
    ? `<p class="server-description">${escapeHtml(server.description)}</p>`
    : "";
  hero.innerHTML = `
    <div class="server-identity">
      <span class="server-avatar" aria-hidden="true">${escapeHtml(serverInitials(server.name))}</span>
      <div class="server-title-stack">
        <h3>${escapeHtml(server.name || "-")}</h3>
        <p>${escapeHtml(server.namespace || "-")}</p>
        ${description}
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
  const inventory = serverDisplayInventory(server);

  const meta = document.createElement("div");
  meta.className = "server-meta-row";
  meta.appendChild(serverMetaPill("HTTP MCP"));
  meta.appendChild(serverMetaPill(`${inventory.tools.length} tools`));
  meta.appendChild(serverMetaPill(`${inventory.prompts.length} prompts`));
  meta.appendChild(serverMetaPill(`${inventory.resources.length} resources`));
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
  if (authenticated && !isTenantUser()) {
    const retireButton = document.createElement("button");
    retireButton.className = "ghost danger server-action";
    retireButton.type = "button";
    retireButton.textContent = "Retire";
    retireButton.addEventListener("click", () => retireServer(server));
    actions.appendChild(retireButton);
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
  const block = document.createElement("details");
  block.className = "inventory-block";
  const summary = document.createElement("summary");
  summary.className = "inventory-section-summary";
  summary.innerHTML = `
    <span>${escapeHtml(label)}</span>
    <small>${formatNumber(items.length)}</small>
  `;
  block.appendChild(summary);

  const body = document.createElement("div");
  body.className = "inventory-section-body";
  if (!items.length) {
    const empty = document.createElement("p");
    empty.className = "inventory-empty";
    empty.textContent = "None";
    body.appendChild(empty);
    block.appendChild(body);
    return block;
  }
  const list = document.createElement("ul");
  items.forEach((item) => {
    const li = document.createElement("li");
    li.innerHTML = itemRenderer(item);
    list.appendChild(li);
  });
  body.appendChild(list);
  block.appendChild(body);
  return block;
}

function renderToolItem(tool) {
  const trust = tool.requiredTrust ? `<span class="trust-chip">${escapeHtml(tool.requiredTrust)}</span>` : "";
  const sideEffect = tool.sideEffect ? `<span class="trust-chip">${escapeHtml(tool.sideEffect)}</span>` : "";
  const drift = renderDriftBadge(tool);
  const labels = renderInventoryLabels(tool.labels);
  return renderExpandableInventoryItem({
    name: tool.name || "-",
    summaryMeta: [trust, sideEffect, drift].filter(Boolean).join(" "),
    description: tool.description,
    labels,
  });
}

function renderInventoryItem(item) {
  if (typeof item === "string") {
    return renderExpandableInventoryItem({ name: item || "-" });
  }
  const drift = renderDriftBadge(item);
  return renderExpandableInventoryItem({
    name: item?.name || item?.uri || "-",
    summaryMeta: drift,
    description: item?.description,
    labels: renderInventoryLabels(item?.labels),
  });
}

function renderDriftBadge(item) {
  if (item?.drift === "ungoverned") {
    return '<span class="drift-chip drift-ungoverned">ungoverned</span>';
  }
  if (item?.drift === "missing") {
    return '<span class="drift-chip drift-missing">missing on server</span>';
  }
  return "";
}

function renderExpandableInventoryItem({ name, summaryMeta = "", description = "", labels = "" }) {
  const details = description
    ? `<p>${escapeHtml(description)}</p>`
    : '<p class="muted-text">No description</p>';
  const labelRows = labels ? `<div class="inventory-label-list">${labels}</div>` : "";
  return `
    <details class="inventory-item">
      <summary>
        <strong>${escapeHtml(name || "-")}</strong>
        ${summaryMeta}
      </summary>
      <div class="inventory-item-body">
        ${details}
        ${labelRows}
      </div>
    </details>
  `;
}

function renderInventoryLabels(labels) {
  if (!labels || typeof labels !== "object") return "";
  return Object.entries(labels)
    .map(([key, value]) => `<span>${escapeHtml(key)}=${escapeHtml(value)}</span>`)
    .join("");
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
  document.getElementById("refresh-user-dashboard")?.addEventListener("click", () => {
    loadUserDashboard();
  });
  document.getElementById("user-analytics-window")?.addEventListener("change", () => {
    loadUserDashboardAnalytics();
  });
  document.getElementById("user-analytics-server")?.addEventListener("change", (event) => {
    selectedUserAnalyticsServerKey = event.target.value || "";
    loadUserDashboardAnalytics();
  });
  document.getElementById("refresh-servers")?.addEventListener("click", () => {
    loadServers();
  });
  document.getElementById("scope-namespace")?.addEventListener("change", (event) => {
    selectedNamespace = (event.target.value || "").trim();
    syncScopeSelector();
    loadActiveTab();
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
    loadServers();
  }, 5000);
}

function isAdminUser() {
  return authPrincipal?.role === "admin";
}

function isTenantUser() {
  return authenticated === true && !isAdminUser();
}

function hasUserIdentity() {
  return authenticated === true && String(authPrincipal?.subject || "").trim() !== "";
}

function applyRoleVisibility() {
  const authRequired = document.querySelectorAll('[data-auth-required="true"]');
  authRequired.forEach((node) => {
    node.classList.toggle("hidden", authenticated !== true);
  });
  const adminOnly = document.querySelectorAll('[data-admin-only="true"]');
  adminOnly.forEach((node) => {
    node.classList.toggle("hidden", !isAdminUser());
  });
  const userOnly = document.querySelectorAll('[data-user-only="true"]');
  userOnly.forEach((node) => {
    node.classList.toggle("hidden", !isTenantUser());
  });
  const userIdentityRequired = document.querySelectorAll('[data-user-identity-required="true"]');
  userIdentityRequired.forEach((node) => {
    node.classList.toggle("hidden", !hasUserIdentity());
  });
  const active = resolveActiveTab();
  activateTab(active);
}

function resolveActiveTab() {
  const active = document.querySelector(".tab.active")?.dataset.tab;
  if (active && isVisibleTab(active)) {
    return active;
  }
  return "servers";
}

function isVisibleTab(tabName) {
  return Array.from(document.querySelectorAll(".tab")).some(
    (tab) => tab.dataset.tab === tabName && !tab.classList.contains("hidden")
  );
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
    const data = await fetchJSON(scopedPath("/runtime/grants"));
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
    } ${g.subject?.agentID || ""} ${g.subject?.teamID || ""}`.toLowerCase();
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
    const namespace = grant.namespace || activeScopeNamespace();
    const serverNamespace = grant.serverRef?.namespace || namespace;

    const row = document.createElement("tr");
    row.appendChild(createIdentityCell(grant.name || "-", namespace));
    row.appendChild(createIdentityCell(grant.serverRef?.name || "-", serverNamespace));
    row.appendChild(createSubjectCell(grant.subject));
    row.appendChild(createGrantRiskCell(grant.maxTrust, grant.allowedSideEffects));
    row.appendChild(createBadgeCell(status, statusClass));
    row.appendChild(
      createActionCell(grant.disabled ? "Enable" : "Disable", () =>
        toggleGrant(namespace, grant.name || "", grant.disabled)
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
  setInlineError("grant-form-error");

  const name = fieldValue("grant-name");
  const server = fieldValue("grant-server");
  if (!name || !server) {
    failGrantForm("Grant name and server are required.");
    return;
  }
  const humanID = fieldValue("grant-human");
  const agentID = fieldValue("grant-agent");
  const teamID = fieldValue("grant-team");
  if (!humanID && !agentID && !teamID) {
    failGrantForm("Provide at least one of Human ID, Agent ID, or Team ID.");
    return;
  }

  let toolRules;
  try {
    toolRules = parseToolRules(fieldValue("grant-tool-rules"));
  } catch (parseErr) {
    failGrantForm(parseErr.message);
    return;
  }
  const sideEffects = selectedGrantSideEffects();
  if (!sideEffects.length) {
    failGrantForm("Select at least one allowed side effect.");
    return;
  }

  if (submit) submit.disabled = true;
  try {
    const payload = {
      name,
      namespace: fieldValue("grant-namespace") || activeScopeNamespace(),
      serverRef: {
        name: server,
        namespace: fieldValue("grant-server-namespace"),
      },
      subject: { humanID, agentID, teamID },
      maxTrust: fieldValue("grant-trust"),
      allowedSideEffects: sideEffects,
      policyVersion: fieldValue("grant-policy-version") || defaults.policyVersion,
      toolRules,
    };
    await fetchJSON("/runtime/grants", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    showToast(`Grant "${payload.name}" applied successfully`);
    focusNamespaceScope(payload.namespace);
    document.getElementById("grant-form")?.reset();
    setFieldValue("grant-namespace", activeScopeNamespace());
    setFieldValue("grant-policy-version", defaults.policyVersion);
    resetGrantSideEffects();
    setInlineError("grant-form-error");
    document.getElementById("grant-form")?.classList.add("hidden");
    loadGrants();
    loadDashboardSummary();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    failGrantForm(`Failed to apply grant: ${readErrorMessage(err, "request failed")}`);
  } finally {
    if (submit) submit.disabled = false;
  }
}

function failGrantForm(message) {
  setInlineError("grant-form-error", message);
  showToast(message, "error");
}

function selectedGrantSideEffects() {
  return Array.from(document.querySelectorAll('input[name="grant-side-effect"]:checked'))
    .map((input) => input.value)
    .filter(Boolean);
}

function resetGrantSideEffects() {
  document.querySelectorAll('input[name="grant-side-effect"]').forEach((input) => {
    input.checked = input.value === "read";
  });
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
    const data = await fetchJSON(scopedPath("/runtime/sessions"));
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
    } ${s.subject?.agentID || ""} ${s.subject?.teamID || ""}`.toLowerCase();
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
    const namespace = session.namespace || activeScopeNamespace();
    const serverNamespace = session.serverRef?.namespace || namespace;

    const row = document.createElement("tr");
    row.appendChild(createIdentityCell(session.name || "-", namespace));
    row.appendChild(createIdentityCell(session.serverRef?.name || "-", serverNamespace));
    row.appendChild(createSubjectCell(session.subject));
    row.appendChild(createTrustCell(session.consentedTrust));
    row.appendChild(createBadgeCell(status, statusClass));
    row.appendChild(
      createActionCell(session.revoked ? "Unrevoke" : "Revoke", () =>
        toggleSession(namespace, session.name || "", session.revoked)
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
  const teamID = fieldValue("session-team");
  if (!humanID && !agentID && !teamID) {
    showToast("Provide at least one of Human ID, Agent ID, or Team ID.", "error");
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
      namespace: fieldValue("session-namespace") || activeScopeNamespace(),
      serverRef: {
        name: server,
        namespace: fieldValue("session-server-namespace"),
      },
      subject: { humanID, agentID, teamID },
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
    focusNamespaceScope(payload.namespace);
    document.getElementById("session-form")?.reset();
    setFieldValue("session-namespace", activeScopeNamespace());
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
  setFieldValue("grant-namespace", activeScopeNamespace());
  setFieldValue("grant-policy-version", defaults.policyVersion);
  setFieldValue("session-namespace", activeScopeNamespace());
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
    setInlineError("grant-form-error");
    document.getElementById("grant-form")?.classList.toggle("hidden");
  });
  document.getElementById("cancel-grant-form")?.addEventListener("click", () => {
    setInlineError("grant-form-error");
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

// Teams
async function loadTeams() {
  setInlineError("team-create-error");
  setInlineError("team-user-error");
  try {
    const data = await fetchJSON("/runtime/teams");
    teamsCache = Array.isArray(data.teams) ? data.teams : [];
    if (!teamsCache.some((team) => team.slug === selectedTeamSlug)) {
      selectedTeamSlug = teamsCache[0]?.slug || "";
    }
    renderTeams();
    renderTeamSelect();
    await loadTeamMembers();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    const message = `Failed to load teams: ${readErrorMessage(err, "request failed")}`;
    teamsCache = [];
    teamMembersCache = [];
    renderTeams();
    renderTeamSelect();
    renderTeamMembers();
    setInlineError("team-create-error", message);
    showToast(message, "error");
  }
}

async function loadTeamMembers() {
  if (!selectedTeamSlug) {
    teamMembersCache = [];
    renderTeamMembers();
    return;
  }
  try {
    const data = await fetchJSON(`/runtime/teams/${encodePathSegment(selectedTeamSlug)}/members`);
    teamMembersCache = Array.isArray(data.members) ? data.members : [];
    renderTeamMembers();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    const message = `Failed to load team users: ${readErrorMessage(err, "request failed")}`;
    teamMembersCache = [];
    renderTeamMembers();
    setInlineError("team-user-error", message);
    showToast(message, "error");
  }
}

function renderTeams() {
  setText("teams-total", formatNumber(teamsCache.length));
  setText("team-members-total", formatNumber(teamMembersCache.length));
  setText("team-selected-count", selectedTeamSlug || "-");

  const tbody = document.getElementById("teams-body");
  if (!tbody) return;
  if (!teamsCache.length) {
    tbody.innerHTML = '<tr><td colspan="4" class="empty">No teams found.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  teamsCache.forEach((team) => {
    const row = document.createElement("tr");
    row.appendChild(createIdentityCell(team.name || team.slug || "-", team.slug || ""));
    row.appendChild(createTextCell(team.namespace || "-"));
    row.appendChild(createCodeCell(team.id || "-"));
    row.appendChild(createTextCell(formatDateTime(team.created_at)));
    row.addEventListener("click", () => {
      selectedTeamSlug = team.slug || "";
      renderTeamSelect();
      loadTeamMembers();
    });
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function renderTeamSelect() {
  const select = document.getElementById("team-user-team");
  if (!select) return;
  select.innerHTML = "";
  if (!teamsCache.length) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "No teams";
    select.appendChild(option);
    select.disabled = true;
    return;
  }
  teamsCache.forEach((team) => {
    const option = document.createElement("option");
    option.value = team.slug || "";
    option.textContent = `${team.slug || "-"} / ${team.namespace || "-"}`;
    select.appendChild(option);
  });
  select.disabled = false;
  select.value = selectedTeamSlug;
}

function renderTeamMembers() {
  setText("team-members-total", formatNumber(teamMembersCache.length));
  setText("team-selected-count", selectedTeamSlug || "-");

  const tbody = document.getElementById("team-members-body");
  if (!tbody) return;
  if (!selectedTeamSlug) {
    tbody.innerHTML = '<tr><td colspan="4" class="empty">Select a team.</td></tr>';
    return;
  }
  if (!teamMembersCache.length) {
    tbody.innerHTML = '<tr><td colspan="4" class="empty">No users in this team.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  teamMembersCache.forEach((member) => {
    const row = document.createElement("tr");
    row.appendChild(createIdentityCell(member.email || member.user_id || "-", member.team_slug || selectedTeamSlug));
    row.appendChild(createBadgeCell(member.role || "member", member.role === "owner" ? "badge-warning" : "badge-muted"));
    row.appendChild(createCodeCell(member.user_id || "-"));
    row.appendChild(createTextCell(formatDateTime(member.created_at)));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

async function createTeam(event) {
  event.preventDefault();
  const submit = event.submitter;
  if (submit?.disabled) return;
  const slug = fieldValue("team-create-slug");
  const name = fieldValue("team-create-name");
  setInlineError("team-create-error");
  if (!slug) {
    const message = "Team slug is required.";
    setInlineError("team-create-error", message);
    showToast(message, "warning");
    return;
  }
  if (submit) submit.disabled = true;
  try {
    await fetchJSON("/runtime/teams", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ slug, name }),
    });
    showToast(`Team "${slug}" created`);
    selectedTeamSlug = slug;
    document.getElementById("team-create-form")?.reset();
    await loadNamespaceScopes();
    await loadTeams();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    const message = `Failed to create team: ${readErrorMessage(err, "request failed")}`;
    setInlineError("team-create-error", message);
    showToast(message, "error");
  } finally {
    if (submit) submit.disabled = false;
  }
}

async function createTeamUser(event) {
  event.preventDefault();
  const submit = event.submitter;
  if (submit?.disabled) return;
  const team = fieldValue("team-user-team") || selectedTeamSlug;
  const email = fieldValue("team-user-email");
  const password = document.getElementById("team-user-password")?.value || "";
  const role = fieldValue("team-user-role") || "member";
  setInlineError("team-user-error");
  if (!team || !email || !password.trim()) {
    const message = "Team, email, and password are required.";
    setInlineError("team-user-error", message);
    showToast(message, "warning");
    return;
  }
  if (submit) submit.disabled = true;
  try {
    await fetchJSON(`/runtime/teams/${encodePathSegment(team)}/users`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, password, role }),
    });
    showToast(`User "${email}" added to ${team}`);
    selectedTeamSlug = team;
    const passwordInput = document.getElementById("team-user-password");
    if (passwordInput) passwordInput.value = "";
    await loadTeamMembers();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    const message = `Failed to create team user: ${readErrorMessage(err, "request failed")}`;
    setInlineError("team-user-error", message);
    showToast(message, "error");
  } finally {
    if (submit) submit.disabled = false;
  }
}

function resetTeams() {
  teamsCache = [];
  teamMembersCache = [];
  selectedTeamSlug = "";
  setText("teams-total", "-");
  setText("team-members-total", "-");
  setText("team-selected-count", "-");
  setInlineError("team-create-error");
  setInlineError("team-user-error");
  const teamsBody = document.getElementById("teams-body");
  if (teamsBody) {
    teamsBody.innerHTML = '<tr><td colspan="4" class="empty">No teams found.</td></tr>';
  }
  const membersBody = document.getElementById("team-members-body");
  if (membersBody) {
    membersBody.innerHTML = '<tr><td colspan="4" class="empty">Select a team.</td></tr>';
  }
  renderTeamSelect();
}

function initTeams() {
  document.getElementById("refresh-teams")?.addEventListener("click", loadTeams);
  document.getElementById("team-create-form")?.addEventListener("submit", createTeam);
  document.getElementById("team-user-form")?.addEventListener("submit", createTeamUser);
  document.getElementById("team-user-team")?.addEventListener("change", (event) => {
    selectedTeamSlug = event.target.value || "";
    loadTeamMembers();
  });
}

// User API Keys
async function loadUserAPIKeys(options = {}) {
  if (!options.preserveOneTime) {
    clearOneTimeUserAPIKey();
  }
  if (!hasUserIdentity()) {
    userAPIKeysCache = [];
    renderUserAPIKeys();
    setInlineError("user-api-key-error", "Sign in with a platform account to manage user API keys.");
    return;
  }
  setInlineError("user-api-key-error");
  try {
    const data = await fetchJSON("/user/api-keys");
    userAPIKeysCache = Array.isArray(data.keys) ? data.keys : [];
    renderUserAPIKeys();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    const message = `Failed to load API keys: ${readErrorMessage(err, "request failed")}`;
    userAPIKeysCache = [];
    renderUserAPIKeys();
    setInlineError("user-api-key-error", message);
    console.error("Failed to load user api keys:", err);
    showToast(message, "error");
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
  setInlineError("user-api-key-error");
  input?.removeAttribute("aria-invalid");
  if (!hasUserIdentity()) {
    const message = "Sign in with a platform account to manage user API keys.";
    setInlineError("user-api-key-error", message);
    showToast(message, "warning");
    return;
  }
  if (!name) {
    const message = "Enter a name for the API key.";
    setInlineError("user-api-key-error", message);
    input?.setAttribute("aria-invalid", "true");
    input?.focus();
    showToast(message, "warning");
    return;
  }
  try {
    const data = await fetchJSON("/user/api-keys", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    const oneTime = document.getElementById("user-api-key-once");
    const cleartextKey = data.one_time_key || data.api_key;
    if (oneTime && cleartextKey) {
      oneTime.textContent = `Copy now (shown once): ${cleartextKey}`;
      oneTime.classList.remove("hidden");
      if (userAPIKeyClearTimer) {
        clearTimeout(userAPIKeyClearTimer);
      }
      userAPIKeyClearTimer = setTimeout(() => {
        clearOneTimeUserAPIKey();
      }, 60000);
    }
    if (input) input.value = "";
    setInlineError("user-api-key-error");
    showToast("API key created");
    await loadUserAPIKeys({ preserveOneTime: true });
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    const message = `Failed to create API key: ${readErrorMessage(err, "request failed")}`;
    setInlineError("user-api-key-error", message);
    showToast(message, "error");
  }
}

async function revokeUserAPIKey(id) {
  if (!hasUserIdentity()) {
    const message = "Sign in with a platform account to manage user API keys.";
    setInlineError("user-api-key-error", message);
    showToast(message, "warning");
    return;
  }
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
  setInlineError("user-api-key-error");
  document.getElementById("user-api-key-name")?.removeAttribute("aria-invalid");
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
  document.getElementById("user-api-key-name")?.addEventListener("input", () => {
    setInlineError("user-api-key-error");
    document.getElementById("user-api-key-name")?.removeAttribute("aria-invalid");
  });
}

// Operations - admin activity and MCP runtime
async function loadMCPOperations() {
  setOperationLoadingState();
  await Promise.allSettled([
    loadOperationServers(),
    loadOperationEvents(),
    loadOperationAudit(),
  ]);
}

function setOperationLoadingState() {
  const serversBody = document.getElementById("ops-server-health-body");
  const activityBody = document.getElementById("ops-activity-body");
  const usersBody = document.getElementById("ops-user-activity-body");
  const userDirectoryBody = document.getElementById("ops-users-body");
  const imageBody = document.getElementById("ops-image-body");
  if (serversBody) {
    serversBody.innerHTML = '<tr><td colspan="6" class="empty">Loading MCP servers...</td></tr>';
  }
  if (activityBody) {
    activityBody.innerHTML = '<tr><td colspan="4" class="empty">Loading MCP activity...</td></tr>';
  }
  if (usersBody) {
    usersBody.innerHTML = '<tr><td colspan="5" class="empty">Loading user activity...</td></tr>';
  }
  if (userDirectoryBody) {
    userDirectoryBody.innerHTML = '<tr><td colspan="6" class="empty">Loading users...</td></tr>';
  }
  if (imageBody) {
    imageBody.innerHTML = '<tr><td colspan="6" class="empty">Loading image activity...</td></tr>';
  }
}

async function loadOperationServers() {
  try {
    operationsServersCache = await loadFleetServers();
    const selectedStillExists = operationsServersCache.some(
      (server) => operationServerKey(server) === selectedOperationsServerKey
    );
    if (!selectedStillExists) {
      selectedOperationsServerKey = operationsServersCache.length
        ? operationServerKey(operationsServersCache[0])
        : "";
    }
    renderOperationsSummary();
    renderOperationServers();
    renderOperationServerSelect();
    renderSelectedOperationServer();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load operation servers:", err);
    operationsServersCache = [];
    renderOperationsSummary();
    const tbody = document.getElementById("ops-server-health-body");
    if (tbody) {
      tbody.innerHTML = '<tr><td colspan="6" class="empty">Error loading MCP servers.</td></tr>';
    }
    renderOperationServerSelect();
    renderSelectedOperationServer();
  }
}

async function loadOperationEvents() {
  try {
    const data = await fetchJSON("/events?limit=20");
    operationsEventsCache = Array.isArray(data.events) ? data.events : [];
    renderOperationsSummary();
    renderOperationEvents();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load operation events:", err);
    operationsEventsCache = [];
    renderOperationsSummary();
    const tbody = document.getElementById("ops-activity-body");
    if (tbody) {
      tbody.innerHTML = '<tr><td colspan="4" class="empty">Error loading MCP activity.</td></tr>';
    }
  }
}

async function loadOperationAudit() {
  try {
    const query = operationFilterQuery();
    const data = await fetchJSON(`/admin/operations?${query}`);
    operationsAuditCache = Array.isArray(data.audit_logs) ? data.audit_logs : [];
    operationsUsersCache = Array.isArray(data.users) ? data.users : [];
    operationsImagesCache = Array.isArray(data.images) ? data.images : [];
    operationsDeploymentsCache = Array.isArray(data.deployments) ? data.deployments : [];
    renderOperationsSummary();
    renderOperationUsers();
    renderUserActivity();
    renderImageActivity();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load user activity:", err);
    operationsAuditCache = [];
    operationsUsersCache = [];
    operationsImagesCache = [];
    operationsDeploymentsCache = [];
    renderOperationsSummary();
    const tbody = document.getElementById("ops-user-activity-body");
    if (tbody) {
      tbody.innerHTML = '<tr><td colspan="5" class="empty">Platform audit is unavailable.</td></tr>';
    }
    const usersBody = document.getElementById("ops-users-body");
    if (usersBody) {
      usersBody.innerHTML = '<tr><td colspan="6" class="empty">User activity is unavailable.</td></tr>';
    }
    const imageBody = document.getElementById("ops-image-body");
    if (imageBody) {
      imageBody.innerHTML = '<tr><td colspan="6" class="empty">Image activity is unavailable.</td></tr>';
    }
  }
}

function operationFilterQuery() {
  const params = new URLSearchParams();
  params.set("limit", "50");
  const user = document.getElementById("ops-filter-user")?.value.trim();
  const since = datetimeLocalToISOString(document.getElementById("ops-filter-since")?.value);
  const until = datetimeLocalToISOString(document.getElementById("ops-filter-until")?.value);
  if (user) params.set("user", user);
  if (since) params.set("since", since);
  if (until) params.set("until", until);
  return params.toString();
}

function datetimeLocalToISOString(value) {
  if (!value) return "";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return "";
  return parsed.toISOString();
}

function renderOperationsSummary() {
  const total = operationsServersCache.length;
  const ready = operationsServersCache.filter((server) => server.status === "Ready").length;
  const loginCount = operationsAuditCache.filter((item) => {
    const action = String(item.action || "");
    return action.includes("login") && item.status === "success";
  }).length;
  setText("ops-mcp-total", formatNumber(total));
  setText("ops-mcp-ready", formatNumber(ready));
  setText("ops-mcp-issues", formatNumber(Math.max(total - ready, 0)));
  setText("ops-login-count", formatNumber(loginCount));
  setText("ops-user-total", formatNumber(operationsUsersCache.length));
  setText("ops-image-count", formatNumber(operationsImagesCache.length));
  setText("ops-deployment-count", formatNumber(operationsDeploymentsCache.length));
  setText("ops-mcp-events", formatNumber(operationsEventsCache.length));
}

function renderOperationUsers() {
  const tbody = document.getElementById("ops-users-body");
  if (!tbody) return;
  if (!operationsUsersCache.length) {
    tbody.innerHTML = '<tr><td colspan="6" class="empty">No users match the current filters.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  operationsUsersCache.forEach((user) => {
    const row = document.createElement("tr");
    row.appendChild(createIdentityCell(user.email || user.id || "-", user.id || ""));
    row.appendChild(createBadgeCell(user.role || "user", user.role === "admin" ? "badge-warning" : "badge-muted"));
    row.appendChild(createTextCell(user.namespace || "-"));
    row.appendChild(createTextCell(formatDateTime(user.last_login_at)));
    row.appendChild(createTextCell(formatDateTime(user.last_activity_at || user.created_at)));
    row.appendChild(createTextCell(formatNumber(user.failed_action_count || 0)));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function renderOperationServers() {
  const tbody = document.getElementById("ops-server-health-body");
  if (!tbody) return;
  if (!operationsServersCache.length) {
    tbody.innerHTML = '<tr><td colspan="6" class="empty">No MCP servers found.</td></tr>';
    return;
  }

  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  operationsServersCache.forEach((server) => {
    const row = document.createElement("tr");
    row.appendChild(createIdentityCell(server.name || "-", server.namespace || "-"));
    row.appendChild(createBadgeCell(server.status || "Unknown", serverBadgeClass(server.status)));
    row.appendChild(createTextCell(server.ready || "0/0"));
    row.appendChild(createTextCell(operationInventoryLabel(server)));
    row.appendChild(createTextCell(formatDateTime(server.age)));
    row.appendChild(createEndpointCell(server.endpoint));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function renderOperationServerSelect() {
  const select = document.getElementById("ops-server-select");
  if (!select) return;
  select.innerHTML = '<option value="">Select server...</option>';
  operationsServersCache.forEach((server) => {
    const option = document.createElement("option");
    option.value = operationServerKey(server);
    option.textContent = `${server.namespace || "-"} / ${server.name || "-"}`;
    select.appendChild(option);
  });
  select.value = selectedOperationsServerKey;
}

function renderSelectedOperationServer() {
  const detail = document.getElementById("ops-server-detail");
  if (!detail) return;
  const server = operationsServersCache.find(
    (item) => operationServerKey(item) === selectedOperationsServerKey
  );
  if (!server) {
    detail.innerHTML = '<div class="empty">Select a server to inspect deployment details.</div>';
    return;
  }

  const labels = server.labels && typeof server.labels === "object"
    ? Object.entries(server.labels)
    : [];
  const inventory = serverDisplayInventory(server);
  const description = server.description
    ? `<p class="server-description">${escapeHtml(server.description)}</p>`
    : "";
  detail.innerHTML = `
    <div class="server-inspector-head">
      <div class="server-identity">
        <span class="server-avatar" aria-hidden="true">${escapeHtml(serverInitials(server.name))}</span>
        <div class="server-title-stack">
          <h3>${escapeHtml(server.name || "-")}</h3>
          <p>${escapeHtml(server.namespace || "-")}</p>
          ${description}
        </div>
      </div>
      <span class="badge ${serverBadgeClass(server.status)}">${escapeHtml(server.status || "Unknown")}</span>
    </div>
    <div class="server-detail-grid">
      ${serverDetailStat("Ready Pods", server.ready || "0/0")}
      ${serverDetailStat("Deployed", formatDateTime(server.age))}
      ${serverDetailStat("Tools", String(inventory.tools.length))}
      ${serverDetailStat("Prompts", String(inventory.prompts.length))}
      ${serverDetailStat("Resources", String(inventory.resources.length))}
      ${serverDetailStat("Tasks", String(inventory.tasks.length))}
    </div>
    <div class="server-detail-block">
      <span class="server-detail-label">Endpoint</span>
      <code class="server-endpoint">${escapeHtml(server.endpoint || "No public endpoint")}</code>
    </div>
    <div class="server-detail-block">
      <span class="server-detail-label">Labels</span>
      <div class="server-label-list">
        ${
          labels.length
            ? labels
                .map(([key, value]) => `<span>${escapeHtml(key)}=${escapeHtml(value)}</span>`)
                .join("")
            : '<span class="muted-text">None</span>'
        }
      </div>
    </div>
  `;
}

function renderOperationEvents() {
  const tbody = document.getElementById("ops-activity-body");
  if (!tbody) return;
  if (!operationsEventsCache.length) {
    tbody.innerHTML = '<tr><td colspan="4" class="empty">No MCP events yet.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  operationsEventsCache.forEach((event) => {
    const row = document.createElement("tr");
    row.innerHTML = `
      <td>${renderAuditTime(event)}</td>
      <td>${renderOperationSubject(event)}</td>
      <td>${renderAuditTarget(event)}</td>
      <td>${renderDecision(event.decision)}</td>
    `;
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function renderUserActivity() {
  const tbody = document.getElementById("ops-user-activity-body");
  if (!tbody) return;
  if (!operationsAuditCache.length) {
    tbody.innerHTML = '<tr><td colspan="5" class="empty">No user activity matches the current filters.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  operationsAuditCache.forEach((item) => {
    const row = document.createElement("tr");
    row.appendChild(createTextCell(formatDateTime(item.created_at)));
    row.appendChild(createIdentityCell(item.email || item.resource || "-", item.namespace || item.actor_ip || ""));
    row.appendChild(createIdentityCell(item.action || "-", auditActivityTarget(item)));
    row.appendChild(createTextCell(item.source || "-"));
    row.appendChild(createBadgeCell(item.status || "unknown", auditStatusBadgeClass(item.status)));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function renderImageActivity() {
  const tbody = document.getElementById("ops-image-body");
  if (!tbody) return;
  if (!operationsImagesCache.length) {
    tbody.innerHTML = '<tr><td colspan="6" class="empty">No image activity matches the current filters.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  operationsImagesCache.forEach((item) => {
    const row = document.createElement("tr");
    row.appendChild(createTextCell(formatDateTime(item.created_at)));
    row.appendChild(createIdentityCell(item.email || item.user_id || "-", item.namespace || ""));
    row.appendChild(createCodeCell(item.image_ref || "-"));
    row.appendChild(createIdentityCell(item.deployment_target || item.server_name || "-", item.source_image || ""));
    row.appendChild(createIdentityCell(item.action || "-", item.source || ""));
    row.appendChild(createBadgeCell(item.status || "unknown", auditStatusBadgeClass(item.status)));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

function auditActivityTarget(item) {
  return [
    item.image_ref,
    item.deployment_target,
    item.server_name,
    item.resource,
    item.message,
  ].find((value) => String(value || "").trim()) || "";
}

function renderOperationSubject(event) {
  const chips = [
    event.human_id ? renderAuditIdentity("Human", event.human_id) : "",
    event.agent_id ? renderAuditIdentity("Agent", event.agent_id) : "",
  ].filter(Boolean);
  return chips.length ? `<div class="subject-chip-list">${chips.join("")}</div>` : '<span class="muted-text">-</span>';
}

function operationServerKey(server) {
  return `${server.namespace || ""}/${server.name || ""}`;
}

function operationInventoryLabel(server) {
  const inventory = serverDisplayInventory(server);
  const pieces = [
    `${inventory.tools.length} tools`,
    `${inventory.prompts.length} prompts`,
    `${inventory.resources.length} resources`,
    `${inventory.tasks.length} tasks`,
  ];
  return pieces.join(" / ");
}

function createEndpointCell(endpoint) {
  const cell = document.createElement("td");
  const code = document.createElement("code");
  code.className = "table-code";
  code.textContent = endpoint || "No public endpoint";
  cell.appendChild(code);
  return cell;
}

function serverDetailStat(label, value) {
  return `
    <div class="server-detail-stat">
      <span>${escapeHtml(label)}</span>
      <strong>${escapeHtml(value || "-")}</strong>
    </div>
  `;
}

function auditStatusBadgeClass(status) {
  if (status === "success") return "badge-success";
  if (status === "denied") return "badge-warning";
  if (status === "error") return "badge-error";
  return "badge-muted";
}

function formatDateTime(value) {
  if (!value) return "-";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function initOperations() {
  document.getElementById("refresh-mcp-ops")?.addEventListener("click", loadMCPOperations);
  document.getElementById("ops-filter-form")?.addEventListener("submit", (event) => {
    event.preventDefault();
    loadMCPOperations();
  });
  document.getElementById("ops-filter-clear")?.addEventListener("click", () => {
    ["ops-filter-user", "ops-filter-since", "ops-filter-until"].forEach((id) => {
      const input = document.getElementById(id);
      if (input) input.value = "";
    });
    loadMCPOperations();
  });
  document.getElementById("ops-server-select")?.addEventListener("change", (event) => {
    selectedOperationsServerKey = event.target.value || "";
    renderSelectedOperationServer();
  });
}

// Platform management
async function loadPlatformManagement() {
  await Promise.allSettled([loadComponents(), loadPlatformServerHealth()]);
}

async function loadPlatformServerHealth() {
  const tbody = document.getElementById("platform-server-health-body");
  if (tbody) {
    tbody.innerHTML = '<tr><td colspan="4" class="empty">Loading MCP server health...</td></tr>';
  }
  try {
    const servers = await loadFleetServers();
    renderPlatformServerHealth(servers);
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load platform server health:", err);
    setText("platform-mcp-total", "-");
    setText("platform-mcp-ready", "-");
    setText("platform-mcp-issues", "-");
    if (tbody) {
      tbody.innerHTML = '<tr><td colspan="4" class="empty">Error loading MCP server health.</td></tr>';
    }
  }
}

async function loadFleetServers() {
  if (authPrincipal?.role !== "admin") {
    const data = await fetchJSON(scopedPath("/runtime/servers"));
    return Array.isArray(data.servers) ? data.servers : [];
  }
  const namespaces = uniqueNonEmpty(
    (Array.isArray(namespaceScopes) ? namespaceScopes : [])
      .map((item) => item?.namespace || "")
  );
  const results = await Promise.all([
    fetchJSON("/runtime/servers").then((data) => Array.isArray(data.servers) ? data.servers : []),
    ...namespaces.map(async (namespace) => {
      const data = await fetchJSON(`/runtime/servers?namespace=${encodeURIComponent(namespace)}`);
      return Array.isArray(data.servers) ? data.servers : [];
    }),
  ]);
  return dedupeServers(results.flat());
}

function uniqueNonEmpty(values) {
  const seen = new Set();
  const out = [];
  values.forEach((value) => {
    const normalized = String(value || "").trim();
    if (!normalized || seen.has(normalized)) return;
    seen.add(normalized);
    out.push(normalized);
  });
  return out;
}

function dedupeServers(servers) {
  const seen = new Set();
  const out = [];
  servers.forEach((server) => {
    const key = operationServerKey(server);
    if (seen.has(key)) return;
    seen.add(key);
    out.push(server);
  });
  out.sort((a, b) => operationServerKey(a).localeCompare(operationServerKey(b)));
  return out;
}

function renderPlatformServerHealth(servers) {
  const total = servers.length;
  const ready = servers.filter((server) => server.status === "Ready").length;
  setText("platform-mcp-total", formatNumber(total));
  setText("platform-mcp-ready", formatNumber(ready));
  setText("platform-mcp-issues", formatNumber(Math.max(total - ready, 0)));

  const tbody = document.getElementById("platform-server-health-body");
  if (!tbody) return;
  if (!servers.length) {
    tbody.innerHTML = '<tr><td colspan="4" class="empty">No MCP servers found.</td></tr>';
    return;
  }
  tbody.innerHTML = "";
  const fragment = document.createDocumentFragment();
  servers.forEach((server) => {
    const row = document.createElement("tr");
    row.appendChild(createIdentityCell(server.name || "-", server.namespace || "-"));
    row.appendChild(createBadgeCell(server.status || "Unknown", serverBadgeClass(server.status)));
    row.appendChild(createTextCell(server.ready || "0/0"));
    row.appendChild(createTextCell(formatDateTime(server.age)));
    fragment.appendChild(row);
  });
  tbody.appendChild(fragment);
}

// Platform - Components
async function loadComponents() {
  const grid = document.getElementById("components-grid");
  if (!grid) return;
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
        <div class="component-meta">${escapeHtml(comp.namespace || "-")} / ${escapeHtml(comp.resource || comp.key || "-")}</div>
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
  if (!select) return;
  const component = select.value;
  setInlineError("restart-component-error");
  select.removeAttribute("aria-invalid");

  if (!component) {
    const message = "Select a component to restart.";
    setInlineError("restart-component-error", message);
    select.setAttribute("aria-invalid", "true");
    select.focus();
    showToast(message, "warning");
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
    setInlineError("restart-component-error");
    setTimeout(loadComponents, 3000);
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    const message = `Failed to restart component: ${readErrorMessage(err, "request failed")}`;
    setInlineError("restart-component-error", message);
    showToast(message, "error");
  }
}

function initPlatform() {
  document.getElementById("refresh-components")?.addEventListener("click", loadPlatformManagement);
  document.getElementById("restart-component-btn")?.addEventListener("click", restartComponent);
  document.getElementById("restart-component-select")?.addEventListener("change", () => {
    setInlineError("restart-component-error");
    document.getElementById("restart-component-select")?.removeAttribute("aria-invalid");
  });
  document.getElementById("open-mcp-operations")?.addEventListener("click", () => {
    activateTab("operations");
    loadMCPOperations();
  });
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
  initTeams();
  initUserAPIKeys();
  initOperations();
  initPlatform();
  initModal();
  initAuth();
});
