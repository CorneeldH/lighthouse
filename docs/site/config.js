// The configuration page: the key map from data/config.json.

import { fillLegend, initTheme, loadJson, renderConfig } from "./map.js?v=cb5549a8";

initTheme(document.getElementById("theme-toggle"));
fillLegend(document.querySelector("[data-sorts]"));

try {
  const [config, packs] = await Promise.all([loadJson("data/config.json"), loadJson("data/packs.json")]);
  document.getElementById("title").textContent = config.title;
  document.getElementById("subtitle").textContent = config.subtitle;
  renderConfig(document.getElementById("config-map"), config, packs, document.getElementById("detail"));
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
