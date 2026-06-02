import { readRuntimeConfig } from "./api/config";
import { LegacyDashboard } from "./components/LegacyDashboard";

export function App() {
  readRuntimeConfig();

  return (
    <main className="app-shell">
      <LegacyDashboard />
    </main>
  );
}
