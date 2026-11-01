// The system page: the three steps and the sections below them, from data/system.json.

import { initTheme, loadJson, renderStory } from "./map.js?v=524a6512";

initTheme(document.getElementById("theme-toggle"));

try {
  const system = await loadJson("data/system.json");
  document.getElementById("title").textContent = system.title;
  document.getElementById("subtitle").textContent = system.subtitle;
  renderStory(document.getElementById("story"), system);
  // The sections exist only now, so the browser could not scroll to a #section in the address yet.
  if (location.hash) {
    document.getElementById(decodeURIComponent(location.hash.slice(1)))?.scrollIntoView();
  }
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
