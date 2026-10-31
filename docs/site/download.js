// The download on the entry page: tabs over the install blocks, the tab of the visitor's
// platform opened, the release label from the GitHub API and a copy button. Without this script
// every block stays visible; it writes text and attributes only, never HTML.

import { choosePlatform, releaseLabel } from "./platform.js";

const API = "https://api.github.com/repos/CorneeldH/lighthouse/releases/latest";
const FALLBACK_TAB = "linux-amd64";

export function initDownload(root) {
  if (!root) {
    return;
  }
  const panels = [...root.querySelectorAll(".platform[data-platform]")];
  if (!panels.length) {
    return;
  }
  const select = buildTabs(panels);
  let touched = false;
  root.addEventListener("click", (event) => {
    if (event.target.closest('[role="tab"], .platform-note-link')) {
      touched = true;
    }
  });

  const note = root.querySelector("#platform-note");
  const info = {
    platform: navigator.userAgentData?.platform ?? navigator.platform ?? "",
    userAgent: navigator.userAgent ?? "",
    architecture: "",
  };
  const apply = (choice) => {
    select(choice.tab ?? FALLBACK_TAB, false);
    showNote(note, choice.note, select);
  };
  apply(choosePlatform(info));
  architecture().then((arch) => {
    if (arch && !touched) {
      apply(choosePlatform({ ...info, architecture: arch }));
    }
  });

  showRelease(root.querySelector("#release-version"));
  addCopyButtons(panels);
}

function buildTabs(panels) {
  const list = document.createElement("div");
  list.className = "platform-tabs";
  list.setAttribute("role", "tablist");
  list.setAttribute("aria-label", "Platform");
  const tabs = panels.map((panel) => {
    const name = panel.dataset.platform;
    panel.id = `platform-${name}`;
    panel.setAttribute("role", "tabpanel");
    const tab = document.createElement("button");
    tab.type = "button";
    tab.id = `tab-${name}`;
    tab.setAttribute("role", "tab");
    tab.setAttribute("aria-controls", panel.id);
    tab.dataset.platform = name;
    tab.textContent = panel.querySelector("h3")?.textContent ?? name;
    panel.setAttribute("aria-labelledby", tab.id);
    list.append(tab);
    return tab;
  });
  panels[0].before(list);

  const select = (name, focus) => {
    tabs.forEach((tab, i) => {
      const on = tab.dataset.platform === name;
      tab.setAttribute("aria-selected", String(on));
      tab.tabIndex = on ? 0 : -1;
      panels[i].hidden = !on;
      if (on && focus) {
        tab.focus();
      }
    });
  };
  list.addEventListener("click", (event) => {
    const tab = event.target.closest('[role="tab"]');
    if (tab) {
      select(tab.dataset.platform, true);
    }
  });
  list.addEventListener("keydown", (event) => {
    const step = { ArrowRight: 1, ArrowLeft: -1 }[event.key];
    if (!step) {
      return;
    }
    const current = tabs.findIndex((tab) => tab.getAttribute("aria-selected") === "true");
    const next = tabs[(current + step + tabs.length) % tabs.length];
    select(next.dataset.platform, true);
    event.preventDefault();
  });
  return select;
}

function showNote(note, kind, select) {
  if (!note) {
    return;
  }
  note.replaceChildren();
  if (kind === "intel") {
    const open = document.createElement("button");
    open.type = "button";
    open.className = "platform-note-link";
    open.textContent = "Take the macOS Intel tab.";
    open.addEventListener("click", () => select("darwin-amd64", true));
    note.append("Intel Mac? ", open);
  } else if (kind === "wsl") {
    note.textContent = "The hooks need WSL: run this inside WSL.";
  }
  note.hidden = !kind;
}

async function architecture() {
  try {
    const values = await navigator.userAgentData?.getHighEntropyValues(["architecture"]);
    return values?.architecture ?? "";
  } catch {
    return "";
  }
}

async function showRelease(label) {
  if (!label) {
    return;
  }
  try {
    const response = await fetch(API, { signal: AbortSignal.timeout(5000) });
    label.textContent = releaseLabel(response.ok ? await response.json() : null);
  } catch {
    // Offline, a time-out or a rate limit: "latest release" stays, and every link still works.
  }
}

function addCopyButtons(panels) {
  if (!navigator.clipboard?.writeText) {
    return;
  }
  for (const panel of panels) {
    const code = panel.querySelector("pre code");
    if (!code) {
      continue;
    }
    const button = document.createElement("button");
    button.type = "button";
    button.className = "copy-button";
    button.textContent = "Copy";
    button.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(code.textContent);
        button.textContent = "Copied";
        setTimeout(() => {
          button.textContent = "Copy";
        }, 2000);
      } catch {
        button.textContent = "Copy failed";
      }
    });
    code.parentElement.before(button);
  }
}
