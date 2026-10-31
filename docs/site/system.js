// The system page: the three steps and the sections below them, from data/system.json.

import { fillLegend, initTheme, loadJson, renderStory } from "./map.js";

initTheme(document.getElementById("theme-toggle"));
fillLegend(document.querySelector("[data-sorts]"));

try {
  const system = await loadJson("data/system.json");
  document.getElementById("title").textContent = system.title;
  document.getElementById("subtitle").textContent = system.subtitle;
  renderStory(document.getElementById("story"), system);
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
