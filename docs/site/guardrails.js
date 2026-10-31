// The guardrails page: the flow map from data/guardrails.json.

import { initTheme, loadJson, renderFlowMap } from "./map.js?v=647b9a73";

initTheme(document.getElementById("theme-toggle"));

try {
  const guardrails = await loadJson("data/guardrails.json");
  document.getElementById("title").textContent = guardrails.title;
  document.getElementById("subtitle").textContent = guardrails.subtitle;
  renderFlowMap(document.getElementById("guardrails-map"), guardrails, document.getElementById("detail"));
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
