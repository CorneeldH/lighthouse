// The reference page: every skill, agent, hook and workflow per pack, from data/packs.json.

import { fillLegend, initTheme, loadJson, renderReference } from "./map.js?v=3f3f67c5";

initTheme(document.getElementById("theme-toggle"));
fillLegend(document.querySelector("[data-sorts]"));

try {
  const packs = await loadJson("data/packs.json");
  for (const root of document.querySelectorAll("[data-part]")) {
    renderReference(root, packs, root.dataset.part);
  }
  document.getElementById("serve-hint").hidden = true;
} catch (error) {
  console.error(error);
  if (location.protocol !== "file:") {
    // Served, so the hint about opening from disk would mislead.
    const hint = document.getElementById("serve-hint");
    hint.textContent = "Could not load the page data.";
    hint.hidden = false;
  }
}
