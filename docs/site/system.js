// The system page: the plan map of the parts from data/system.json, with the date of the snapshot.

import { drawerFor, initTheme, loadJson, renderPlanMap, renderTimeline } from "./map.js?v=87b4b61c";

initTheme(document.getElementById("theme-toggle"));

try {
  const system = await loadJson("data/system.json");
  document.getElementById("title").textContent = system.title;
  document.getElementById("subtitle").textContent = system.subtitle;
  document.getElementById("snapshot").textContent = `Snapshot of ${system.snapshot}`;
  const detail = document.getElementById("detail");
  const planRoot = document.getElementById("plan-map");
  const timelineRoot = document.getElementById("timeline");
  const plan = renderPlanMap(planRoot, system, detail);
  renderTimeline(timelineRoot, system, detail);

  const nowButton = document.getElementById("switch-now");
  const timeButton = document.getElementById("switch-time");
  const switchTo = (now) => {
    drawerFor(detail).close();
    planRoot.hidden = !now;
    timelineRoot.hidden = now;
    nowButton.setAttribute("aria-pressed", String(now));
    timeButton.setAttribute("aria-pressed", String(!now));
    (now ? nowButton : timeButton).focus();
    if (now) {
      plan.redraw();
    }
  };
  nowButton.addEventListener("click", () => switchTo(true));
  timeButton.addEventListener("click", () => switchTo(false));
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
