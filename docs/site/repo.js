// The repo page: the floor plan of a repo after init, from data/repo.json.

import { initTheme, loadJson, renderRepo } from "./map.js?v=d3a74df1";

initTheme(document.getElementById("theme-toggle"));

try {
  const repo = await loadJson("data/repo.json");
  document.getElementById("title").textContent = repo.title;
  document.getElementById("subtitle").textContent = repo.subtitle;
  renderRepo(document.getElementById("repo-map"), repo, document.getElementById("detail"));
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
