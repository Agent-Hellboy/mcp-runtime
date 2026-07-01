import React from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App";
import "./styles/global.css";

const root = document.getElementById("root");

if (!root) {
  throw new Error("missing #root");
}

function loadRuntimeConfig(): Promise<void> {
  if (window.MCP_API_BASE !== undefined) {
    return Promise.resolve();
  }

  return new Promise((resolve) => {
    const script = document.createElement("script");
    script.src = "/config.js";
    script.onload = () => resolve();
    script.onerror = () => resolve();
    document.head.appendChild(script);
  });
}

loadRuntimeConfig().then(() => {
  createRoot(root).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>
  );
});
