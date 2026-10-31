// The hooks page: the flow map from data/hooks.json.

import { initTheme, loadJson, renderFlowMap } from "./map.js";

initTheme(document.getElementById("theme-toggle"));

try {
  const hooks = await loadJson("data/hooks.json");
  document.getElementById("title").textContent = hooks.title;
  document.getElementById("subtitle").textContent = hooks.subtitle;
  renderFlowMap(document.getElementById("hooks-map"), hooks, document.getElementById("detail"));
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
