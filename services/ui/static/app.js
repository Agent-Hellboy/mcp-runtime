const apiBase = window.MCP_API_BASE || "/api";

// API Helper
async function fetchJSON(path, options = {}) {
  const headers = { ...options.headers };
  if (window.MCP_API_KEY) {
    headers["x-api-key"] = window.MCP_API_KEY;
  }

  const response = await fetch(`${apiBase}${path}`, {
    ...options,
    headers,
  });

  if (!response.ok) {
    const error = await response.text();
    throw new Error(error || `Request failed: ${response.status}`);
  }

  return response.json();
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

    // Also update old hero card for backward compatibility
    if (document.getElementById("total-events")) {
      document.getElementById("total-events").textContent = formatNumber(
        data.total_events || 0
      );
    }
  } catch (err) {
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

function initDashboard() {
  loadDashboardSummary();
  loadEvents();

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

  startAutoRefresh();
}

function startAutoRefresh() {
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
    const tbody = document.getElementById("grants-body");
    const filter = document.getElementById("grant-filter")?.value.toLowerCase() || "";

    if (!data.grants || data.grants.length === 0) {
      tbody.innerHTML =
        '<tr><td colspan="6" class="empty">No grants found.</td></tr>';
      return;
    }

    const filtered = data.grants.filter((g) => {
      if (!filter) return true;
      const search = `${g.serverRef?.name} ${g.subject?.humanID} ${g.subject?.agentID}`.toLowerCase();
      return search.includes(filter);
    });

    if (filtered.length === 0) {
      tbody.innerHTML =
        '<tr><td colspan="6" class="empty">No grants match filter.</td></tr>';
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
  } catch (err) {
    console.error("Failed to load grants:", err);
    document.getElementById("grants-body").innerHTML =
      '<tr><td colspan="6" class="empty">Error loading grants.</td></tr>';
  }
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
    showToast(`Failed to ${action} grant: ${err.message}`, "error");
  }
}

// Governance - Sessions
async function loadSessions() {
  try {
    const data = await fetchJSON("/runtime/sessions");
    const tbody = document.getElementById("sessions-body");
    const filter =
      document.getElementById("session-filter")?.value.toLowerCase() || "";

    if (!data.sessions || data.sessions.length === 0) {
      tbody.innerHTML =
        '<tr><td colspan="6" class="empty">No sessions found.</td></tr>';
      return;
    }

    const filtered = data.sessions.filter((s) => {
      if (!filter) return true;
      const search = `${s.serverRef?.name} ${s.subject?.humanID} ${s.subject?.agentID}`.toLowerCase();
      return search.includes(filter);
    });

    if (filtered.length === 0) {
      tbody.innerHTML =
        '<tr><td colspan="6" class="empty">No sessions match filter.</td></tr>';
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
  } catch (err) {
    console.error("Failed to load sessions:", err);
    document.getElementById("sessions-body").innerHTML =
      '<tr><td colspan="6" class="empty">Error loading sessions.</td></tr>';
  }
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
    showToast(`Failed to ${action} session: ${err.message}`, "error");
  }
}

function initGovernance() {
  document.getElementById("refresh-grants")?.addEventListener("click", loadGrants);
  document.getElementById("refresh-sessions")?.addEventListener("click", loadSessions);
  const debouncedLoadGrants = debounce(loadGrants, 200);
  const debouncedLoadSessions = debounce(loadSessions, 200);

  document.getElementById("grant-filter")?.addEventListener("input", debouncedLoadGrants);
  document.getElementById("session-filter")?.addEventListener("input", debouncedLoadSessions);
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
});
