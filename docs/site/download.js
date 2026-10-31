// The download on the install page: the block of the visitor's platform opened and moved to the
// top, the release label from the GitHub API and a copy button. Without this script every block
// stays folded in; it writes text and attributes only, never HTML.

import { choosePlatform, releaseLabel } from "./platform.js?v=87b4b61c";

const API = "https://api.github.com/repos/CorneeldH/lighthouse/releases/latest";

export function initDownload(root) {
  if (!root) {
    return;
  }
  const panels = [...root.querySelectorAll(".platform[data-platform]")];
  if (!panels.length) {
    return;
  }
  const select = buildSelect(root, panels);
  let touched = false;
  root.addEventListener("click", (event) => {
    if (event.target.closest("summary, .platform-note-link")) {
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
    select(choice.tab);
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

// select(name) opens that block and no other, and puts it first, then the "Other platforms"
// heading, then the rest. An unknown or null name changes nothing.
function buildSelect(root, panels) {
  const others = document.createElement("h2");
  others.className = "others-title";
  others.textContent = "Other platforms";
  return (name) => {
    const target = panels.find((panel) => panel.dataset.platform === name);
    if (!target) {
      return;
    }
    for (const panel of panels) {
      panel.open = panel === target;
    }
    const first = root.querySelector(".platform[data-platform]");
    if (first !== target) {
      first.before(target);
    }
    target.after(others);
  };
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
    open.textContent = "Open the macOS Intel build.";
    open.addEventListener("click", () => select("darwin-amd64"));
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
