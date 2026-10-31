// The chain page: the flow map from data/chain.json and the pack cards from data/packs.json.

import { initTheme, loadJson, renderFlowMap } from "./map.js";

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) {
    node.className = className;
  }
  if (text !== undefined) {
    node.textContent = text;
  }
  return node;
}

function count(items, singular, plural) {
  return `${items.length} ${items.length === 1 ? singular : plural}`;
}

function listItem(kind, tag, text) {
  const item = el("li");
  item.append(el("span", `tag kind-${kind}`, tag), text);
  return item;
}

function packCard(pack) {
  const card = el("div", "box");
  const when = pack.proposed_when === "always" ? "always" : `proposed when: ${pack.proposed_when}`;
  card.append(el("p", "kicker", when), el("h3", "", pack.name));
  const list = el("ul");
  if (pack.skills.length) {
    list.append(listItem("lighthouse", count(pack.skills, "skill", "skills"), pack.skills.join(", ")));
  }
  if (pack.agents.length) {
    list.append(listItem("lighthouse", count(pack.agents, "agent", "agents"), pack.agents.join(", ")));
  }
  if (pack.workflows.length) {
    list.append(
      listItem("github", count(pack.workflows, "workflow", "workflows"), pack.workflows.join(", ")),
    );
  }
  if (pack.hooks.length) {
    const hooks = el("ul");
    for (const hook of pack.hooks) {
      hooks.append(el("li", "", `${hook.name}: ${hook.when}`));
    }
    const item = listItem("hook", count(pack.hooks, "hook", "hooks"), "");
    item.append(hooks);
    list.append(item);
  }
  for (const extra of pack.extra) {
    list.append(el("li", "", extra));
  }
  card.append(list);
  return card;
}

initTheme(document.getElementById("theme-toggle"));

try {
  const [chain, packs] = await Promise.all([loadJson("data/chain.json"), loadJson("data/packs.json")]);
  document.getElementById("serve-hint").hidden = true;
  document.getElementById("title").textContent = chain.title;
  document.getElementById("subtitle").textContent = chain.subtitle;
  renderFlowMap(document.getElementById("chain-map"), chain, document.getElementById("detail"));
  document.getElementById("pack-grid").replaceChildren(...packs.map(packCard));
} catch (error) {
  // The serve hint stays in view; the console says what failed.
  console.error(error);
}
