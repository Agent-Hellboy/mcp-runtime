const apiBase = window.MCP_API_BASE || "/api";
const defaults = Object.assign(
  { namespace: "mcp-servers", policyVersion: "v1" },
  window.MCP_DEFAULTS || {}
);
let authenticated = null;
let grantsCache = [];
let sessionsCache = [];

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
      showAuthModal("Enter a valid API key to continue.");
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
  const contents = document.querySelectorAll(".tab-content");

  tabs.forEach((tab) => {
    tab.addEventListener("click", () => {
      const target = tab.dataset.tab;

      if (authenticated !== true) {
        if (authenticated === false) {
          showAuthModal();
        }
        return;
      }

      tabs.forEach((t) => {
        const isActive = t === tab;
        t.classList.toggle("active", isActive);
        t.setAttribute("aria-selected", String(isActive));
      });

      contents.forEach((content) => {
        const isActive = content.id === `tab-${target}`;
        content.classList.toggle("active", isActive);
        content.hidden = !isActive;
      });

      // Load data when switching to certain tabs
      if (target === "governance") {
        loadGrants();
        loadSessions();
      } else if (target === "operations") {
        loadComponents();
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

  try {
    const response = await fetch("/auth/status", { credentials: "same-origin" });
    const data = await response.json();
    setAuthenticated(Boolean(data.authenticated));
  } catch (err) {
    console.error("Failed to check auth status:", err);
    setAuthenticated(false);
  }

  if (authenticated) {
    loadActiveTab();
    startAutoRefresh();
  } else {
    showAuthModal();
  }
}

async function handleAuthSubmit(event) {
  event.preventDefault();
  const input = document.getElementById("api-key-input");
  const submit = document.getElementById("auth-submit");
  const apiKey = input?.value || "";

  setAuthError("");
  if (submit) submit.disabled = true;
  try {
    const response = await fetch("/auth/login", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ api_key: apiKey }),
    });
    if (!response.ok) {
      throw new Error(await authFailureMessage(response));
    }
    if (input) input.value = "";
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

async function authFailureMessage(response) {
  let serverError = "";
  try {
    const body = await response.json();
    serverError = body?.error || "";
  } catch (_) {
    // Non-JSON failures still get a useful status-based message below.
  }

  if (response.status === 401) {
    return "Invalid API key";
  }
  if (response.status === 503 && serverError === "api_key_not_configured") {
    return "Server is not configured for API key auth";
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
  setAuthenticated(false);
  resetDashboard();
  showAuthModal();
}

function setAuthenticated(value) {
  authenticated = value;
  document.getElementById("auth-open")?.classList.toggle("hidden", value);
  document.getElementById("auth-logout")?.classList.toggle("hidden", !value);
}

function showAuthModal(message = "") {
  stopAutoRefresh();
  setAuthError(message);
  const modal = document.getElementById("auth-modal");
  modal?.classList.remove("hidden");
  setTimeout(() => document.getElementById("api-key-input")?.focus(), 0);
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
  const active = document.querySelector(".tab.active")?.dataset.tab || "dashboard";
  if (active === "dashboard") {
    loadDashboardSummary();
    loadEvents();
  } else if (active === "governance") {
    loadGrants();
    loadSessions();
  } else if (active === "operations") {
    loadComponents();
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
}

function startAutoRefresh() {
  if (!authenticated) return;
  if (autoRefreshInterval) return;
  const autoRefreshCheckbox = document.getElementById("auto-refresh");
  if (autoRefreshCheckbox && !autoRefreshCheckbox.checked) return;
  autoRefreshInterval = setInterval(() => {
    loadDashboardSummary();
    loadEvents();
  }, 5000);
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
  initOperations();
  initModal();
  initAuth();
});
