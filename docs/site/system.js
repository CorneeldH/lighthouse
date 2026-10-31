// The system page: the plan map of the parts from data/system.json, with the date of the snapshot.

import { initTheme, loadJson, renderPlanMap } from "./map.js";

initTheme(document.getElementById("theme-toggle"));

try {
  const system = await loadJson("data/system.json");
  document.getElementById("title").textContent = system.title;
  document.getElementById("subtitle").textContent = system.subtitle;
  document.getElementById("snapshot").textContent = `Snapshot of ${system.snapshot}`;
  renderPlanMap(document.getElementById("plan-map"), system, document.getElementById("detail"));
  document.getElementById("serve-hint").hidden = true;
} catch (error) {
  console.error(error);
  if (location.protocol !== "file:") {
    // Served, so the hint about opening from disk would mislead.
    const hint = document.getElementById("serve-hint");
    hint.textContent = "Could not load the map data.";
    hint.hidden = false;
  }
}
