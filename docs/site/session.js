// The session page: the flow map from data/session.json.

import { fillLegend, initTheme, loadJson, renderFlowMap } from "./map.js";

initTheme(document.getElementById("theme-toggle"));
fillLegend(document.querySelector("[data-sorts]"));

try {
  const session = await loadJson("data/session.json");
  document.getElementById("title").textContent = session.title;
  document.getElementById("subtitle").textContent = session.subtitle;
  renderFlowMap(document.getElementById("session-map"), session, document.getElementById("detail"));
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
