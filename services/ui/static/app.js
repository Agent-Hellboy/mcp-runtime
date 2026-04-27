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
      if (target === "governance") {
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
        '<tr><td colspan="5" class="empty">No events yet.</td></tr>';
      return;
    }

    tbody.innerHTML = "";
    const fragment = document.createDocumentFragment();

    data.events.forEach((event) => {
      const row = document.createElement("tr");
      row.innerHTML = `
        <td>${new Date(event.timestamp).toLocaleString()}</td>
        <td>${escapeHtml(event.source || "-")}</td>
        <td>${escapeHtml(event.event_type || "-")}</td>
        <td>${escapeHtml(event.server || "-")}</td>
        <td>${renderDecision(event.decision)}</td>
      `;
      fragment.appendChild(row);
    });

    tbody.appendChild(fragment);
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load events:", err);
  }
}

function renderDecision(decision) {
  if (!decision) return "-";
  const color =
    decision === "allow"
      ? "var(--success)"
      : decision === "deny"
      ? "var(--error)"
      : "var(--muted)";
  return `<span style="color: ${color}; font-weight: 600;">${escapeHtml(
    decision
  )}</span>`;
}

function escapeHtml(text) {
  if (!text) return "";
  const div = document.createElement("div");
  div.textContent = text;
  return div.innerHTML;
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

function createBadgeCell(text, className) {
  const cell = document.createElement("td");
  const badge = document.createElement("span");
  badge.className = `badge ${className}`;
  badge.textContent = text;
  cell.appendChild(badge);
  return cell;
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
  document.getElementById("dash-total-events").textContent = "-";
  document.getElementById("dash-active-servers").textContent = "-";
  document.getElementById("dash-active-grants").textContent = "-";
  document.getElementById("dash-active-sessions").textContent = "-";
  document.getElementById("events-body").innerHTML =
    '<tr><td colspan="5" class="empty">No events yet.</td></tr>';
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
  if (serversCache.length === 0) {
    grid.innerHTML = '<div class="component-card">No MCP servers found.</div>';
    return;
  }

  grid.innerHTML = "";
  const fragment = document.createDocumentFragment();
  serversCache.forEach((server) => {
    const card = document.createElement("article");
    card.className = "server-card";

    const title = document.createElement("div");
    title.className = "server-card-head";
    title.innerHTML = `
      <div>
        <h3>${escapeHtml(server.name || "-")}</h3>
        <p>${escapeHtml(server.namespace || "-")}</p>
      </div>
      <span class="badge ${server.status === "Ready" ? "badge-success" : "badge-muted"}">${escapeHtml(server.status || "Unknown")}</span>
    `;
    card.appendChild(title);

    if (server.endpoint) {
      const endpoint = document.createElement("code");
      endpoint.className = "server-endpoint";
      endpoint.textContent = server.endpoint;
      card.appendChild(endpoint);
    }

    card.appendChild(renderInventoryBlock("Tools", server.tools || [], renderToolItem));
    card.appendChild(renderInventoryBlock("Prompts", server.prompts || [], renderInventoryItem));
    card.appendChild(renderInventoryBlock("Resources", server.resources || [], renderInventoryItem));
    card.appendChild(renderInventoryBlock("Tasks", server.tasks || [], renderInventoryItem));

    const json = document.createElement("pre");
    json.className = "access-json";
    json.textContent = JSON.stringify(server.access_json || {}, null, 2);
    card.appendChild(json);

    fragment.appendChild(card);
  });
  grid.appendChild(fragment);
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
    empty.textContent = "None declared.";
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
  const trust = tool.requiredTrust ? ` <span>${escapeHtml(tool.requiredTrust)}</span>` : "";
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
  document.getElementById("refresh-servers")?.addEventListener("click", () => {
    loadServers();
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
    renderGrants();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load grants:", err);
    grantsCache = [];
    document.getElementById("grants-body").innerHTML =
      '<tr><td colspan="6" class="empty">Error loading grants.</td></tr>';
  }
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
    const subject = grant.subject?.humanID || grant.subject?.agentID || "-";
    const status = grant.disabled ? "Disabled" : "Active";
    const statusClass = grant.disabled ? "badge-muted" : "badge-success";

    const row = document.createElement("tr");
    row.appendChild(createTextCell(grant.name || "-"));
    row.appendChild(createTextCell(grant.serverRef?.name || "-"));
    row.appendChild(createTextCell(subject));
    row.appendChild(createTextCell(grant.maxTrust || "-"));
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
    renderSessions();
  } catch (err) {
    if (isUnauthorizedError(err)) return;
    console.error("Failed to load sessions:", err);
    sessionsCache = [];
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
    const subject =
      session.subject?.humanID || session.subject?.agentID || "-";
    const status = session.revoked ? "Revoked" : "Active";
    const statusClass = session.revoked ? "badge-error" : "badge-success";

    const row = document.createElement("tr");
    row.appendChild(createTextCell(session.name || "-"));
    row.appendChild(createTextCell(session.serverRef?.name || "-"));
    row.appendChild(createTextCell(subject));
    row.appendChild(createTextCell(session.consentedTrust || "-"));
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
