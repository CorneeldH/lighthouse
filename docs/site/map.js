// Shared flow map of the lighthouse site: columns of chips, an "Always on" band, route buttons,
// handoff badges and a detail drawer. Data reaches the page only through textContent and
// attributes, never as HTML, so a label such as issue-<nr>-<slug> shows as written.

const GLYPHS = { you: "⌨", skill: "↪", description: "◎", tool: "⚡", event: "◷", github: "⚙" };
const AGENT_GLYPH = "◆"; // an agent keeps its own mark, whatever starts it
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
const THEME_KEY = "lighthouse-theme";

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

/** The mark on a chip: the agent mark for an agent, otherwise the mark of its trigger. */
function glyphOf(chip) {
  return form(chip) === "agent" ? AGENT_GLYPH : GLYPHS[chip.trigger] || "?";
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
  if (chip.phase !== "always") {
    button.style.width = `calc(${100 / columns}% - 14px)`;
  }
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
 * Renders a flow map into `root`: route buttons with the note of the chosen route, columns of
 * chips, the "Always on" band and the handoff badges. A click on a chip or badge opens `detail`
 * as a drawer from the right; Escape or its close button shuts it.
 */
export function renderFlowMap(root, data, detail) {
  const phases = data.phases || [];
  const chips = data.chips || [];
  const routes = data.routes || [];
  const handoffs = data.handoffs || [];
  const columnOf = new Map(phases.map((phase, i) => [phase.id, i]));
  const phaseLabel = new Map(phases.map((phase) => [phase.id, phase.label]));
  phaseLabel.set("always", "Always on");
  const byId = new Map(chips.map((chip) => [chip.id, chip]));
  const placed = chips
    .filter((chip) => columnOf.has(chip.phase))
    .sort((a, b) => columnOf.get(a.phase) - columnOf.get(b.phase) || a.row - b.row);
  const always = chips.filter((chip) => chip.phase === "always").sort((a, b) => a.row - b.row);
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
  let current = null;
  let chosen = null;
  let reshow = null;
  const mark = (node) => {
    if (current) {
      current.classList.remove("current");
    }
    current = node;
    node.classList.add("current");
  };

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
    reshow = null;
  };
  const open = (boxes, focus = true) => {
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
    mark(button);
    // Another route changes the neighbours in "In the chain", so a route click redraws it.
    reshow = () => showChip(chip, button, false);
    const glyph = glyphOf(chip);
    const kind = form(chip);
    open(
      [
        box(
          `${phaseLabel.get(chip.phase) || chip.phase} · ${kind} · ${GLYPHS[chip.trigger] || "?"} ${TRIGGERS[chip.trigger] || chip.trigger}`,
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
    );
  };

  const showHandoff = (handoff, button) => {
    mark(button);
    const from = byId.get(handoff.from);
    const to = byId.get(handoff.to);
    const about = data.handoff || {};
    reshow = null;
    open([
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

  if (always.length) {
    const band = el("div", "band");
    band.append(el("p", "band-title", "Always on"));
    for (const chip of always) {
      const button = chipButton(chip, phases.length);
      button.addEventListener("click", () => showChip(chip, button));
      chipButtons.push(button);
      band.append(button);
    }
    map.append(band);
  }
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
      // Always-on chips take part in every route, so they never dim.
      const always = byId.get(button.dataset.id)?.phase === "always";
      button.classList.toggle("dim", !always && !used.has(button.dataset.id));
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
