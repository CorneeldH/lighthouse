// Shared flow map of the lighthouse site: columns of chips, route buttons, handoff badges and a
// detail drawer. SORTS is the one table of glyphs and labels, used by chips and legends. Data reaches the page only through textContent and
// attributes, never as HTML, so a label such as issue-<nr>-<slug> shows as written.

/** The sorts of thing on the maps, each with one glyph and one label. The glyph says what a thing
    is; what starts it is in the title, the drawer and the screen-reader text. */
export const SORTS = {
  program: { glyph: "⛯", label: "lighthouse, the program" },
  "workflow-skill": { glyph: "↪", label: "Workflow skill" },
  "standard-skill": { glyph: "✓", label: "Standard skill" },
  agent: { glyph: "◆", label: "Agent" },
  hook: { glyph: "⚡", label: "Hook" },
  rules: { glyph: "§", label: "Rules" },
  config: { glyph: "⚙", label: "Config" },
  github: { glyph: "⑂", label: "GitHub" },
  loop: { glyph: "∞", label: "The loop" },
  session: { glyph: "❯", label: "A session" },
  you: { glyph: "▣", label: "You bring it" },
};
const TRIGGERS = {
  you: "you invoke it",
  skill: "another skill invokes it",
  description: "its description matches the task",
  tool: "a tool call",
  event: "a turn event",
  github: "a GitHub event",
};
// A trigger in the terms of the harness docs. A hook has none: its column already is its event.
const STARTS = {
  you: "User-invoked",
  skill: "Invoked by another skill",
  description: "Model-invoked",
  github: "GitHub event",
};
// What a hook does when it fires.
const EFFECTS = {
  blocks: "blocks the step",
  warns: "warns, does not block",
  formats: "formats the file",
  records: "records state for a later hook",
};
const FORMS = {
  skill: "Skill",
  agent: "Agent",
  hook: "Hook",
  workflow: "Workflow",
  github: "GitHub",
  tool: "External tool",
  dispatcher: "Dispatcher",
};
const WIDTH = 1260; // virtual width of the map, equal to the min-width of .map in map.css
const HEAD = 64; // height of the column heads, in px
const ROW = 46; // height of one row of chips, in px
const SVG_NS = "http://www.w3.org/2000/svg";
const LINE_INSET = 24; // a line down runs this far, in px, inside the right side of its boxes
const LINE_STAGGER = 16; // and each next link this much further left, so no two lines coincide
const THEME_KEY = "lighthouse-theme";
const REPO_BLOB = "https://github.com/CorneeldH/lighthouse/blob/main/";
const REPO_TREE = "https://github.com/CorneeldH/lighthouse/tree/main/";
const ISSUE_URL = "https://github.com/CorneeldH/lighthouse/issues/";
const drawers = new WeakMap(); // detail element -> its drawer

/** Fetches and parses a JSON file; throws when the request fails. */
export async function loadJson(url) {
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`${url}: HTTP ${response.status}`);
  }
  return response.json();
}

function readTheme() {
  try {
    const value = window.localStorage.getItem(THEME_KEY);
    return value === "dark" || value === "light" ? value : null;
  } catch {
    return null;
  }
}

function writeTheme(value) {
  try {
    window.localStorage.setItem(THEME_KEY, value);
  } catch {
    // No storage (private window, blocked site data): the switch works, without memory.
  }
}

/** Follows prefers-color-scheme; the button switches the theme and remembers the choice. */
export function initTheme(button) {
  const root = document.documentElement;
  const prefersDark = window.matchMedia("(prefers-color-scheme: dark)");
  const stored = readTheme();
  if (stored) {
    root.dataset.theme = stored;
  }
  const current = () => root.dataset.theme || (prefersDark.matches ? "dark" : "light");
  const label = () => {
    button.textContent = current() === "dark" ? "Light theme" : "Dark theme";
  };
  label();
  prefersDark.addEventListener("change", label);
  button.addEventListener("click", () => {
    const next = current() === "dark" ? "light" : "dark";
    root.dataset.theme = next;
    writeTheme(next);
    label();
  });
}

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

/** "#<issue>" or the milestone of a planned item; empty otherwise. */
function plannedMark(item) {
  if (item.status !== "planned") {
    return "";
  }
  if (item.issue) {
    return `#${item.issue}`;
  }
  return item.milestone || "";
}

function statusText(item) {
  let text = item.status;
  const mark = plannedMark(item);
  if (mark) {
    text += `, ${mark}`;
  } else if (item.status === "idea") {
    text += ", no issue yet";
  }
  if (item.not_in_first_release) {
    text += " · not in first release";
  }
  return text;
}

/** The pack and the part (skill, agent, hook, ...) a source URL under pack/ points to. */
function sourceParts(source) {
  const match = /\/blob\/main\/pack\/([^/]+)\/([^/]+)\//.exec(source || "");
  return match ? { pack: match[1], part: match[2].replace(/s$/, "") } : null;
}

/** skill, agent, hook, workflow or github: what kind of thing the chip is. */
function form(chip) {
  if (chip.form) {
    return chip.form;
  }
  const parts = sourceParts(chip.source);
  if (chip.kind === "hook") {
    return "hook";
  }
  if (chip.kind === "superpowers") {
    return "skill";
  }
  if (parts && ["skill", "agent", "workflow"].includes(parts.part)) {
    return parts.part;
  }
  return "github";
}

// A form of the flow maps that is no sort of its own: a GitHub Actions workflow is GitHub, an
// external tool is something you bring, the dispatcher is the lighthouse program.
const SORT_OF_FORM = { workflow: "github", tool: "you", dispatcher: "program" };

/** The key of SORTS an item belongs to. */
export function sortOf(item) {
  const kind = form(item);
  if (kind === "skill") {
    return item.trigger === "description" ? "standard-skill" : "workflow-skill";
  }
  return SORT_OF_FORM[kind] || kind;
}

/** The mark on a chip: the glyph of its sort. */
export function glyphOf(item) {
  const sort = SORTS[sortOf(item)];
  return sort ? sort.glyph : "?";
}

/** Fills a legend list with one item per sort named in its data-sorts, as "<glyph> <label>". */
export function fillLegend(list) {
  const items = list.dataset.sorts.split(/\s+/).filter(Boolean).map((name) => {
    if (!SORTS[name]) {
      throw new Error(`unknown sort in legend: ${name}`);
    }
    return el("li", "", `${SORTS[name].glyph} ${SORTS[name].label}`);
  });
  list.replaceChildren(...items);
}

// The colour kind of each sort on the reference page; an agent adds its own colour by form-agent.
const KIND_OF_SORT = {
  "workflow-skill": "lighthouse",
  "standard-skill": "lighthouse",
  agent: "lighthouse",
  hook: "hook",
  github: "github",
};

// Per part of a pack: the names it lists, the sort of each and the repo path of its place.
const REFERENCE_PARTS = {
  skills: (pack, name) => ({
    sort: pack.standard_skills.includes(name) ? "standard-skill" : "workflow-skill",
    href: `${REPO_TREE}pack/${pack.name}/skills/${name}`,
  }),
  agents: (pack, name) => ({ sort: "agent", href: `${REPO_BLOB}pack/${pack.name}/agents/${name}.md` }),
  hooks: (pack, name) => ({ sort: "hook", href: `${REPO_BLOB}pack/${pack.name}/hooks/${name}.sh` }),
  workflows: (pack, name) => ({
    sort: "github",
    href: `${REPO_BLOB}pack/${pack.name}/workflows/${name}.yml`,
  }),
};

/** The parts of one kind per pack, in the order of packs.json: [{pack, items: [{name, sort, href}]}].
    A pack without a part of that kind is left out. `part` is skills, agents, hooks or workflows. */
export function referenceEntries(packs, part) {
  const make = REFERENCE_PARTS[part];
  if (!make) {
    throw new Error(`unknown part: ${part}`);
  }
  return packs
    .map((pack) => ({
      pack: pack.name,
      items: pack[part].map((entry) => {
        const name = part === "hooks" ? entry.name : entry;
        return { name, ...make(pack, name) };
      }),
    }))
    .filter((entry) => entry.items.length > 0);
}

/** Per pack a heading and a list of links to the parts of one kind, each with its glyph. */
export function renderReference(root, packs, part) {
  const groups = referenceEntries(packs, part).map((entry) => {
    const group = el("div", "reference-pack");
    group.append(el("h3", "", entry.pack));
    const list = el("ul", "reference-list");
    for (const item of entry.items) {
      const link = el("a", `reference-link kind-${KIND_OF_SORT[item.sort]} form-${item.sort}`);
      link.href = item.href;
      link.append(el("span", "reference-glyph", SORTS[item.sort].glyph), ` ${item.name}`);
      const row = el("li");
      row.append(link);
      list.append(row);
    }
    group.append(list);
    return group;
  });
  root.replaceChildren(...groups);
}

/** One item of the system page: a link with the glyph of its sort, its label and its note. */
function storyItem(item) {
  const link = el("a", `story-item kind-${item.kind} form-${sortOf(item)}`);
  link.href = item.href;
  const glyph = el("span", "glyph", glyphOf(item));
  glyph.setAttribute("aria-hidden", "true");
  const text = el("span", "story-text");
  text.append(el("span", "story-label", item.label));
  if (item.note) {
    text.append(el("span", "story-note", item.note));
  }
  link.append(glyph, text);
  return link;
}

/**
 * Renders the system page into `root`: the steps side by side, each with its number, its title
 * (a command as a terminal line) and its items, an item with a `where` of its own apart at the
 * bottom of its step. Below them every section as a box, its groups as a list of terms.
 */
export function renderStory(root, data) {
  const story = el("ol", "story");
  data.steps.forEach((step, index) => {
    const node = el("li", "story-step");
    node.append(el("p", "kicker", `Step ${index + 1}`));
    const title = el("h2", "story-title");
    if (step.command) {
      title.append(el("code", "story-command", `$ ${step.command}`));
    } else {
      title.textContent = step.title;
    }
    node.append(title);
    if (step.where) {
      node.append(el("p", "story-where", step.where));
    }
    const list = el("ul", "story-items");
    for (const item of step.items.filter((item) => !item.where)) {
      const row = el("li");
      row.append(storyItem(item));
      list.append(row);
    }
    node.append(list);
    for (const item of step.items.filter((item) => item.where)) {
      const apart = el("div", "story-apart");
      apart.append(el("p", "story-where", item.where), storyItem(item));
      node.append(apart);
    }
    story.append(node);
  });
  const sections = data.sections.map((section) => {
    const node = el("section", "box story-section");
    node.id = section.id;
    node.append(el("h2", "", section.title), ...section.text.map((text) => el("p", "", text)));
    if (section.groups) {
      node.append(facts(section.groups.map((group) => [group.label, group.tools.join(", ")])));
    }
    return node;
  });
  root.replaceChildren(story, ...sections);
}

/** `how` as a list of steps; a single string stays one paragraph. */
function steps(how) {
  if (!Array.isArray(how)) {
    return el("p", "", how || "");
  }
  const list = el("ul", "steps");
  for (const step of how) {
    list.append(el("li", "", step));
  }
  return list;
}

function provider(kind, source, kindOfPart) {
  if (kindOfPart === "tool") {
    return "an external tool that lighthouse runs";
  }
  const parts = sourceParts(source);
  const pack = parts ? `pack ${parts.pack}` : "";
  switch (kind) {
    case "lighthouse":
      return parts ? `lighthouse, ${parts.part} in ${pack}` : "lighthouse";
    case "superpowers":
      return "the Superpowers plugin";
    case "hook":
      return parts ? `a lighthouse hook in ${pack}` : "a lighthouse hook";
    case "github":
      return parts ? `GitHub, with the ${parts.part} from lighthouse ${pack}` : "GitHub";
    default:
      return kind;
  }
}

function sourceLink(source) {
  if (!source || !source.startsWith("https://")) {
    return null;
  }
  const link = el("a", "", "Read the full definition");
  link.href = source;
  return link;
}

function facts(rows) {
  const list = el("dl");
  for (const [term, value] of rows) {
    if (value === null || value === undefined || value === "") {
      continue;
    }
    const dd = el("dd");
    dd.append(value);
    list.append(el("dt", "", term), dd);
  }
  return list;
}

function box(kicker, heading, ...body) {
  const node = el("div", "box");
  if (kicker) {
    node.append(el("p", "kicker", kicker));
  }
  if (heading) {
    node.append(el("h2", "", heading));
  }
  node.append(...body);
  return node;
}

/** matcher · pack · effects: the one line a hook chip carries. */
function hookSummary(chip) {
  const parts = sourceParts(chip.source);
  return `${chip.matcher || "all"} · ${parts ? parts.pack : "?"} · ${chip.effects.join(" + ")}`;
}

function chipButton(chip, columns) {
  const button = el("button", `chip kind-${chip.kind} form-${form(chip)} status-${chip.status}`);
  button.type = "button";
  button.dataset.id = chip.id;
  button.title = `${chip.label}: ${TRIGGERS[chip.trigger] || chip.trigger}, ${chip.status}`;
  button.style.width = `calc(${100 / columns}% - 14px)`;
  const glyph = el("span", "glyph", glyphOf(chip));
  glyph.setAttribute("aria-hidden", "true");
  button.append(glyph, el("span", "label", chip.label));
  const hookLine = chip.command ? hookSummary(chip) : "";
  button.append(
    el("span", "sr-only", ` (${TRIGGERS[chip.trigger] || chip.trigger}, ${chip.status}${hookLine ? `, ${hookLine}` : ""})`),
  );
  const mark = plannedMark(chip);
  if (mark) {
    button.append(el("span", "mark", mark));
  }
  if (chip.not_in_first_release) {
    button.append(el("span", "release", "not in first release"));
  }
  if (hookLine) {
    button.append(el("span", "hook", hookLine));
  }
  return button;
}

/** A curve through the points of a route, as in the mockup: a bulge within one column. */
function routePath(points) {
  if (!points.length) {
    return "";
  }
  let d = `M ${points[0].x} ${points[0].y}`;
  for (let i = 1; i < points.length; i++) {
    const a = points[i - 1];
    const b = points[i];
    if (Math.abs(a.x - b.x) < 5) {
      d += ` C ${a.x + 55} ${a.y}, ${b.x + 55} ${b.y}, ${b.x} ${b.y}`;
    } else {
      const m = (a.x + b.x) / 2;
      d += ` C ${m} ${a.y}, ${m} ${b.y}, ${b.x} ${b.y}`;
    }
  }
  return d;
}

/** The last box of the drawer: a link to the file that defines the part, when there is one. */
function definition(source) {
  const link = sourceLink(source);
  if (!link) {
    return [];
  }
  const text = el("p");
  text.append(link);
  return [box("Definition", "", text)];
}

/** The labels of `ids` as a comma-separated list, or `none` when there are none. */
function labels(ids, byId, none) {
  return ids.length ? ids.map((id) => byId.get(id)?.label || id).join(", ") : none;
}

/**
 * The drawer on `detail`, one per element however often it is asked for: the first call makes
 * it (tabindex and one Escape listener), later calls return the same object and so the same
 * state. `show(node, boxes, focus, onClose)` marks `node` as current, fills `detail` with a
 * Close button and `boxes`, opens it and focuses it when `focus`. `close()` shuts it, hands the
 * focus back to `node` and calls the `onClose` of the last `show`.
 */
export function drawerFor(detail) {
  const known = drawers.get(detail);
  if (known) {
    return known;
  }
  let current = null;
  let onClose = null;
  const close = () => {
    if (!detail.classList.contains("open")) {
      return;
    }
    detail.classList.remove("open");
    if (current) {
      current.classList.remove("current");
      current.focus();
      current = null;
    }
    if (onClose) {
      onClose();
    }
  };
  const show = (node, boxes, focus = true, closed = null) => {
    if (current) {
      current.classList.remove("current");
    }
    current = node;
    node.classList.add("current");
    onClose = closed;
    const shut = el("button", "detail-close", "Close");
    shut.type = "button";
    shut.setAttribute("aria-label", "Close details");
    shut.addEventListener("click", close);
    detail.replaceChildren(shut, ...boxes);
    detail.classList.add("open");
    if (focus) {
      detail.focus();
    }
  };
  detail.tabIndex = -1;
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      close();
    }
  });
  const drawer = { show, close };
  drawers.set(detail, drawer);
  return drawer;
}

/**
 * Renders a flow map into `root`: route buttons with the note of the chosen route, columns of
 * chips and the handoff badges. A click on a chip or badge opens `detail`
 * as a drawer from the right; Escape or its close button shuts it.
 */
export function renderFlowMap(root, data, detail) {
  const phases = data.phases || [];
  const chips = data.chips || [];
  const routes = data.routes || [];
  const handoffs = data.handoffs || [];
  const columnOf = new Map(phases.map((phase, i) => [phase.id, i]));
  const phaseLabel = new Map(phases.map((phase) => [phase.id, phase.label]));
  const byId = new Map(chips.map((chip) => [chip.id, chip]));
  const placed = chips
    .filter((chip) => columnOf.has(chip.phase))
    .sort((a, b) => columnOf.get(a.phase) - columnOf.get(b.phase) || a.row - b.row);
  const rows = Math.max(-1, ...placed.map((chip) => chip.row)) + 1;
  const height = HEAD + rows * ROW;
  const columnWidth = WIDTH / Math.max(phases.length, 1);
  const point = (chip) => ({
    x: (columnOf.get(chip.phase) + 0.5) * columnWidth,
    y: HEAD + chip.row * ROW + ROW / 2,
  });
  const percent = (x) => `${(x / WIDTH) * 100}%`;

  const chipButtons = [];
  const badges = [];
  let chosen = null;
  let reshow = null;
  const drawer = drawerFor(detail);

  /** Where `chip` sits on the routes: which ones, and its neighbours on the chosen one. */
  const routeFacts = (chip) => {
    const on = routes.filter((route) => route.chips.includes(chip.id));
    const names = on.map((route) =>
      (route.examples || []).includes(chip.id) ? `${route.label}, as an example` : route.label,
    );
    const rows = [["On routes", names.join("; ") || "none"]];
    if (chosen && chosen.chips.includes(chip.id)) {
      const before = new Set();
      const after = new Set();
      chosen.chips.forEach((id, i) => {
        if (id !== chip.id) {
          return;
        }
        if (i > 0) {
          before.add(chosen.chips[i - 1]);
        }
        if (i < chosen.chips.length - 1) {
          after.add(chosen.chips[i + 1]);
        }
      });
      rows.push([`Before it on ${chosen.label}`, labels([...before], byId, "the route starts here")]);
      rows.push([`After it on ${chosen.label}`, labels([...after], byId, "the route ends here")]);
    }
    return rows;
  };

  const showChip = (chip, button, focus = true) => {
    // Another route changes the neighbours in "In the chain", so a route click redraws it.
    reshow = () => showChip(chip, button, false);
    const glyph = glyphOf(chip);
    const kind = form(chip);
    drawer.show(
      button,
      [
        box(
          `${phaseLabel.get(chip.phase) || chip.phase} · ${kind} · ${TRIGGERS[chip.trigger] || chip.trigger}`,
          `${glyph} ${chip.label}`,
          el("p", "summary", chip.what),
        ),
        box(
          "When, who and why",
          "",
          facts([
            ["What it is", FORMS[kind]],
            ...(kind === "hook" ? [] : [["When it starts", STARTS[chip.trigger] || chip.trigger]]),
            ["Who provides it", provider(chip.kind, chip.source, kind)],
            ...(chip.command
              ? [
                  ["Matcher", chip.matcher || (chip.trigger === "event" ? "none: this event has no matcher" : "none: every call")],
                  ["Command", chip.command],
                  ["What it does", chip.effects.map((e) => EFFECTS[e]).join("; ")],
                ]
              : []),
            ["What you miss without", chip.without],
            ["Status", statusText(chip)],
          ]),
        ),
        box("How it works", "", steps(chip.how)),
        box("In the chain", "", facts(routeFacts(chip))),
        ...definition(chip.source),
      ],
      focus,
      () => {
        reshow = null;
      },
    );
  };

  const showHandoff = (handoff, button) => {
    const from = byId.get(handoff.from);
    const to = byId.get(handoff.to);
    const about = data.handoff || {};
    reshow = null;
    drawer.show(button, [
      box(
        `handoff · ${phaseLabel.get(from.phase)} → ${phaseLabel.get(to.phase)}`,
        "↻ handoff",
        el("p", "summary", about.what || ""),
        el("p", "aside", `Between ${from.label} and ${to.label}.`),
      ),
      box(
        "When, who and why",
        "",
        facts([
          ["When it starts", about.when],
          ["Who provides it", provider("lighthouse", about.source)],
          ["What you miss without", about.without],
          ["Status", statusText(handoff)],
        ]),
      ),
      box("How it works", "", steps(about.how)),
      ...definition(about.source),
    ]);
  };

  // Route buttons and the note of the chosen route.
  const routeBar = el("div", "routes");
  routeBar.setAttribute("role", "group");
  routeBar.setAttribute("aria-label", "Routes");
  const note = el("p", "route-note");
  note.hidden = true;
  const examples = el("p", "route-note");
  examples.hidden = true;
  const hint = el(
    "p",
    "map-hint",
    "Click a chip or a handoff for what it does, when it starts and who provides it.",
  );

  // The map: column backgrounds, route curves, chips and badges.
  const scroll = el("div", "map-scroll");
  const map = el("div", "map");
  const area = el("div", "map-area");
  area.style.height = `${height}px`;
  phases.forEach((phase, i) => {
    const column = el("div", "column");
    column.style.left = `calc(${percent(i * columnWidth)} + 6px)`;
    column.style.width = `calc(${percent(columnWidth)} - 12px)`;
    const head = el("div", "column-head", `${i + 1} · ${phase.label}`);
    head.append(el("span", "", phase.blurb || ""));
    column.append(head);
    area.append(column);
  });

  const svg = document.createElementNS(SVG_NS, "svg");
  svg.setAttribute("viewBox", `0 0 ${WIDTH} ${height}`);
  svg.setAttribute("preserveAspectRatio", "none");
  svg.setAttribute("aria-hidden", "true");
  const paths = routes.map(() => {
    const path = document.createElementNS(SVG_NS, "path");
    svg.append(path);
    return path;
  });
  // With the roadmap off, a route line runs past the planned steps it would otherwise visit.
  const drawRoutes = (roadmap) => {
    routes.forEach((route, i) => {
      const points = route.chips
        .map((id) => byId.get(id))
        .filter((chip) => chip && columnOf.has(chip.phase))
        .filter((chip) => roadmap || chip.status === "works")
        .map(point);
      paths[i].setAttribute("d", routePath(points));
    });
    if (chosen) {
      casing.setAttribute("d", paths[routes.indexOf(chosen)].getAttribute("d"));
    }
  };
  // Drawn under the chosen route only, so its yellow keeps an edge on a light map.
  const casing = document.createElementNS(SVG_NS, "path");
  casing.classList.add("casing");
  drawRoutes(false);
  area.append(svg);

  const badgeFor = (handoff) => {
    const from = byId.get(handoff.from);
    const to = byId.get(handoff.to);
    if (!from || !to || !columnOf.has(from.phase) || !columnOf.has(to.phase)) {
      return null;
    }
    const a = point(from);
    const b = point(to);
    // Within a phase: centred in the column, just above the step it hands over to, so an example
    // step in between stays before it. Across phases: halfway, on a row of chips half a row
    // lower, which keeps it off the first gap where the handoffs within a phase sit.
    let row = from.phase === to.phase ? to.row - 0.5 : (from.row + to.row) / 2;
    if (Number.isInteger(row)) {
      row += 0.5;
    }
    const tag = plannedMark(handoff);
    const badge = el("button", `handoff status-${handoff.status}`, tag ? `↻ handoff · ${tag}` : "↻ handoff");
    badge.type = "button";
    badge.dataset.from = handoff.from;
    badge.dataset.to = handoff.to;
    badge.title = `handoff from ${from.label} to ${to.label}: ${statusText(handoff)}`;
    badge.style.left = percent((a.x + b.x) / 2);
    badge.style.top = `${HEAD + row * ROW + ROW / 2}px`;
    badge.addEventListener("click", () => showHandoff(handoff, badge));
    badges.push(badge);
    return badge;
  };

  for (const chip of placed) {
    const button = chipButton(chip, phases.length);
    const p = point(chip);
    button.style.left = percent(p.x);
    button.style.top = `${p.y}px`;
    button.addEventListener("click", () => showChip(chip, button));
    chipButtons.push(button);
    area.append(button);
    // A badge follows the chip it leaves from, so Tab meets it in reading order.
    for (const handoff of handoffs.filter((h) => h.from === chip.id)) {
      const badge = badgeFor(handoff);
      if (badge) {
        area.append(badge);
      }
    }
  }
  map.append(area);
  scroll.append(map);

  const routeButtons = routes.map((route, i) => {
    const button = el("button", "", route.label);
    button.type = "button";
    button.setAttribute("aria-pressed", "false");
    button.addEventListener("click", () => select(i));
    routeBar.append(button);
    return button;
  });

  function select(index) {
    const route = routes[index];
    chosen = route;
    const used = new Set(route.chips);
    routeButtons.forEach((button, i) => button.setAttribute("aria-pressed", String(i === index)));
    paths.forEach((path, i) => path.classList.toggle("on", i === index));
    // The chosen route goes on top, so a route drawn later does not cross over it.
    casing.setAttribute("d", paths[index].getAttribute("d"));
    svg.append(casing, paths[index]);
    for (const button of chipButtons) {
      button.classList.toggle("dim", !used.has(button.dataset.id));
    }
    for (const badge of badges) {
      // Lit when the route passes both ends in order, also with an example step in between.
      const from = route.chips.indexOf(badge.dataset.from);
      const to = route.chips.indexOf(badge.dataset.to);
      badge.classList.toggle("dim", from < 0 || to < 0 || from > to);
    }
    note.textContent = route.note || "";
    note.hidden = !route.note;
    const shown = (route.examples || []).map((id) => byId.get(id)?.label || id);
    examples.textContent = shown.length
      ? `Examples on this route, which join in only when the work calls for them: ${shown.join(", ")}.`
      : "";
    examples.hidden = !shown.length;
    if (reshow) {
      reshow();
    }
  }

  // The roadmap (planned and idea) stays off the map until asked for: the map shows what runs.
  if ([...chips, ...handoffs].some((item) => item.status !== "works")) {
    const toggle = el("label", "roadmap-toggle");
    const check = el("input");
    check.type = "checkbox";
    check.addEventListener("change", () => {
      map.classList.toggle("show-roadmap", check.checked);
      drawRoutes(check.checked);
    });
    toggle.append(check, " Show roadmap");
    routeBar.append(toggle);
  }

  root.replaceChildren(routeBar, note, examples, hint, scroll);
  if (routes.length) {
    select(0);
  }
}

/** The issues of the part `partId` and of every part below it, in the order of the data. */
function issuesOf(data, partId) {
  const ids = new Set([partId]);
  let grown = true;
  while (grown) {
    grown = false;
    for (const part of data.parts || []) {
      if (part.parent && ids.has(part.parent) && !ids.has(part.id)) {
        ids.add(part.id);
        grown = true;
      }
    }
  }
  return (data.issues || []).filter((issue) => ids.has(issue.part));
}

/** The title of milestone `number`, as GitHub names it; empty when the data does not know it. */
function milestoneTitle(data, number) {
  return (data.milestones || []).find((milestone) => milestone.number === number)?.title || "";
}

/** "works", or "planned, <milestone title>", plus " · not in first release" when so marked. */
function planStatus(item, data) {
  let text = item.status;
  const title = item.status === "planned" ? milestoneTitle(data, item.milestone) : "";
  if (title) {
    text += `, ${title}`;
  }
  if (item.not_in_first_release) {
    text += " · not in first release";
  }
  return text;
}

function issueLink(issue, text) {
  const link = el("a", "", text);
  link.href = `${ISSUE_URL}${issue.issue}`;
  return link;
}

/** The drawer of a part: what it does, when it runs, its status and its issues. */
function showPart(drawer, part, data, node) {
  const parent = (data.parts || []).find((other) => other.id === part.parent);
  const issues = issuesOf(data, part.id);
  const works = issues.filter((issue) => issue.status === "works").length;
  let list;
  if (issues.length) {
    list = el("ul", "plan-issues");
    for (const issue of issues) {
      const item = el("li");
      item.append(issueLink(issue, `#${issue.issue} ${issue.title}`), ` · ${issue.status}`);
      list.append(item);
    }
  } else {
    list = el("p", "", "No issues on this part yet.");
  }
  drawer.show(node, [
    box(parent ? `In ${parent.label}` : "", part.label, el("p", "summary", part.what)),
    box(
      "",
      "",
      facts([
        ["When it runs", part.when],
        ["What you miss without", part.without],
        ["Status", planStatus(part, data)],
      ]),
    ),
    box(`Issues (${works}/${issues.length})`, "", list),
    ...definition(part.source),
  ]);
}

/** The drawer of an issue: its status, milestone and part, and a link to it on GitHub. */
function showIssue(drawer, issue, data, node) {
  const part = (data.parts || []).find((other) => other.id === issue.part);
  const open = el("p");
  open.append(issueLink(issue, `Open #${issue.issue} on GitHub`));
  drawer.show(node, [
    box(`Issue #${issue.issue}`, issue.title),
    box(
      "",
      "",
      facts([
        ["Status", issue.status],
        ["Milestone", milestoneTitle(data, issue.milestone)],
        ["Part", part?.label || issue.part],
      ]),
      open,
    ),
  ]);
}

/**
 * Renders the plan map into `root`: every part as a box in a grid, a part with parts below it as
 * a container around them, and the links between parts as thin lines on top. A click on a part
 * opens `detail` as a drawer. `redraw()` measures the boxes again and moves the lines; it does
 * nothing while `root` is hidden.
 */
export function renderPlanMap(root, data, detail) {
  const parts = data.parts || [];
  const links = data.links || [];
  const byId = new Map(parts.map((part) => [part.id, part]));
  const boxes = new Map(); // part id -> the element a line ends on
  const drawer = drawerFor(detail);

  const partButton = (part, className) => {
    const button = el("button", `${className} status-${part.status}`);
    button.type = "button";
    button.dataset.id = part.id;
    button.append(el("span", "plan-label", part.label));
    if (part.status === "planned") {
      const title = milestoneTitle(data, part.milestone);
      if (title) {
        button.append(el("span", "plan-mark", title));
      }
    }
    button.append(el("span", "sr-only", ` (${part.status})`));
    if (part.not_in_first_release) {
      button.append(el("span", "release", "not in first release"));
    }
    button.addEventListener("click", () => showPart(drawer, part, data, button));
    return button;
  };

  // A part with parts below it is a container: its own head button (or, for a group, a title
  // without a button) sits next to the buttons of its parts, never around them.
  const build = (part) => {
    const children = parts.filter((other) => other.parent === part.id);
    if (!children.length) {
      const button = partButton(part, "plan-part");
      boxes.set(part.id, button);
      return button;
    }
    const shell = el("div", part.group ? "plan-shell plan-group" : "plan-shell");
    shell.append(
      part.group ? el("p", "plan-title", part.label) : partButton(part, "plan-part plan-head"),
    );
    shell.append(...children.map(build));
    boxes.set(part.id, shell);
    return shell;
  };

  const plan = el("div", "plan");
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.classList.add("plan-lines");
  svg.setAttribute("aria-hidden", "true");
  const drawn = links
    .filter((link) => byId.has(link.from) && byId.has(link.to))
    .map((link) => {
      const line = document.createElementNS(SVG_NS, "line");
      const text = document.createElementNS(SVG_NS, "text");
      text.textContent = link.label;
      if (byId.get(link.from).status !== "works" || byId.get(link.to).status !== "works") {
        line.classList.add("planned");
        text.classList.add("planned");
      }
      svg.append(line, text);
      return { link, line, text };
    });
  plan.append(...parts.filter((part) => !part.parent).map(build), svg);

  function redraw() {
    if (root.offsetParent === null) {
      return;
    }
    const origin = plan.getBoundingClientRect();
    const measure = (id) => {
      const r = boxes.get(id).getBoundingClientRect();
      const left = r.left - origin.left;
      const top = r.top - origin.top;
      return {
        left,
        top,
        right: left + r.width,
        bottom: top + r.height,
        x: left + r.width / 2,
        y: top + r.height / 2,
      };
    };
    drawn.forEach(({ link, line, text }, index) => {
      const a = measure(link.from);
      const b = measure(link.to);
      // Facing sides: left and right when the centres lie further apart across than down. The
      // centres, not the gaps: a part low in a tall container still faces sideways. Across, the
      // line runs between the middles of the facing sides, with its label centred on it.
      // Down, as in one column where every box is full width, the middles would all share one x
      // and the lines would run into each other. So a line down runs straight, at an x inside the
      // horizontal overlap of both boxes, near its right side and staggered per link. Its label
      // sits left of it, over the empty right end of the buttons, clear of their text.
      const across = Math.abs(b.x - a.x) > Math.abs(b.y - a.y);
      let start;
      let end;
      if (across) {
        start = { x: b.x > a.x ? a.right : a.left, y: a.y };
        end = { x: b.x > a.x ? b.left : b.right, y: b.y };
      } else {
        const x = Math.max(
          Math.max(a.left, b.left) + 8,
          Math.min(a.right, b.right) - LINE_INSET - LINE_STAGGER * index,
        );
        start = { x, y: b.y > a.y ? a.bottom : a.top };
        end = { x, y: b.y > a.y ? b.top : b.bottom };
      }
      line.setAttribute("x1", start.x);
      line.setAttribute("y1", start.y);
      line.setAttribute("x2", end.x);
      line.setAttribute("y2", end.y);
      text.classList.toggle("beside", !across);
      text.setAttribute("x", across ? (start.x + end.x) / 2 : start.x - 6);
      text.setAttribute("y", (start.y + end.y) / 2);
    });
  }

  root.replaceChildren(
    el("p", "map-hint", "Click a part for what it does, what you miss without it, and its issues."),
    plan,
  );
  new ResizeObserver(redraw).observe(root);
  redraw();
  return { redraw };
}

/**
 * Renders the timeline into `root`: the issues grouped per milestone, in the order of the data,
 * with a filter that dims the issues outside one part. A click on an issue opens `detail`.
 */
export function renderTimeline(root, data, detail) {
  const drawer = drawerFor(detail);
  const chips = []; // { chip, issue }

  const filter = el("div", "timeline-filter");
  const label = el("label", "", "Show part");
  const select = el("select");
  select.id = "timeline-part";
  label.htmlFor = select.id;
  const all = el("option", "", "All parts");
  all.value = "";
  select.append(all);
  for (const part of data.parts || []) {
    if (part.group) {
      continue;
    }
    const option = el("option", "", part.label);
    option.value = part.id;
    select.append(option);
  }
  filter.append(label, select);

  const groups = (data.milestones || []).map((milestone) => {
    const issues = (data.issues || []).filter((issue) => issue.milestone === milestone.number);
    const works = issues.filter((issue) => issue.status === "works").length;
    const group = el("section", "timeline-group");
    const head = el("h2", "", milestone.title);
    head.append(el("span", "timeline-count", `${works}/${issues.length}`));
    group.append(head);
    if (!issues.length) {
      group.append(el("p", "timeline-empty", "no issues yet"));
    }
    for (const issue of issues) {
      const chip = el("button", `plan-chip status-${issue.status}`, `#${issue.issue} ${issue.title}`);
      chip.append(el("span", "sr-only", ` (${issue.status})`));
      chip.type = "button";
      chip.addEventListener("click", () => showIssue(drawer, issue, data, chip));
      chips.push({ chip, issue });
      group.append(chip);
    }
    return group;
  });

  select.addEventListener("change", () => {
    const shown = select.value ? new Set(issuesOf(data, select.value)) : null;
    for (const { chip, issue } of chips) {
      chip.classList.toggle("dim", shown !== null && !shown.has(issue));
    }
  });

  root.replaceChildren(filter, ...groups);
}
