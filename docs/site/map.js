// Shared flow map of the lighthouse site: columns of chips, route buttons, handoff badges and a
// detail drawer. SORTS is the one table of glyphs and labels, used by chips and legends. Data
// reaches the page only through textContent and attributes, never as HTML, so a label such as
// issue-<nr>-<slug> shows as written.

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
const THEME_KEY = "lighthouse-theme";
const REPO_BLOB = "https://github.com/CorneeldH/lighthouse/blob/main/";
const REPO_TREE = "https://github.com/CorneeldH/lighthouse/tree/main/";
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
  program: "lighthouse",
};

// Per part of a pack: the names it lists, the sort of each and the repo path of its place.
const REFERENCE_PARTS = {
  skills: (pack, name) => ({
    sort: pack.standard_skills.includes(name) ? "standard-skill" : "workflow-skill",
    href: `${REPO_TREE}pack/${pack.name}/skills/${name}`,
  }),
  agents: (pack, name) => ({ sort: "agent", href: `${REPO_BLOB}pack/${pack.name}/agents/${name}.md` }),
  // A hook that runs as `lighthouse hook <name>` names its Go source; any other is a script.
  hooks: (pack, name, entry) => ({
    sort: "hook",
    href: entry.source ? `${REPO_BLOB}${entry.source}` : `${REPO_BLOB}pack/${pack.name}/hooks/${name}.sh`,
  }),
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
        return { name, ...make(pack, name, entry) };
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
      const glyph = el("span", "reference-glyph", SORTS[item.sort].glyph);
      glyph.setAttribute("aria-hidden", "true");
      link.append(glyph, ` ${item.name}`);
      const row = el("li");
      row.append(link);
      list.append(row);
    }
    group.append(list);
    return group;
  });
  root.replaceChildren(...groups);
}

const CONFIG_LEVELS = { set: "Set this", optional: "Optional", advanced: "Advanced" };

// The sort of a reader by the folder of its file under pack/<pack>/.
const CONFIG_READER_SORTS = { hooks: "hook", workflows: "github", agents: "agent" };

/** The name, sort and link of one reader of a key, from its repo path. A skill folder links to its
    tree, everything else to its file. */
export function configReader(path, packs) {
  const [root, packName, folder, name] = path.split("/");
  const file = path.split("/").pop();
  if (root === "pack" && folder === "skills" && name) {
    const pack = packs.find((entry) => entry.name === packName);
    const standard = pack && pack.standard_skills.includes(name);
    return { name, sort: standard ? "standard-skill" : "workflow-skill", href: REPO_TREE + path };
  }
  const sort = (root === "pack" && CONFIG_READER_SORTS[folder]) || "program";
  return { name: file, sort, href: REPO_BLOB + path };
}

/** One value as TOML: a string quoted, a list in brackets, a table inline. */
export function tomlValue(value) {
  if (Array.isArray(value)) {
    return `[${value.map(tomlValue).join(", ")}]`;
  }
  if (value !== null && typeof value === "object") {
    const pairs = Object.entries(value).map(
      ([name, item]) => `${/^[A-Za-z0-9_-]+$/.test(name) ? name : JSON.stringify(name)} = ${tomlValue(item)}`,
    );
    return `{ ${pairs.join(", ")} }`;
  }
  return typeof value === "string" ? JSON.stringify(value) : String(value);
}

/** `text` as comment lines of at most `width` characters. */
function commentLines(text, width = 80) {
  const lines = [];
  let line = "#";
  for (const word of text.split(" ")) {
    if (line.length > 1 && line.length + 1 + word.length > width) {
      lines.push(line);
      line = "#";
    }
    line += ` ${word}`;
  }
  return [...lines, line];
}

// The comment behind a key starts in one column per table. A key longer than this does not push
// that column out: its comment starts on the next line, in the same column.
const CONFIG_COLUMN = 40;
// A comment wraps at this column.
const CONFIG_RIGHT = 100;

/** One file of the configuration page as TOML lines, without a DOM: per table a heading comment
    with its title, its header and one line per key, set to its default or commented out with its
    example when it has none, and what it does as a comment behind it, aligned per table. A blank
    line parts two tables. A derived key is left out: nobody writes it. [{text, key, note}]: `key`
    only on the line of a key, `note` the index where its comment starts on that line. */
export function configToml(file) {
  const lines = [];
  for (const table of file.tables) {
    const keys = table.keys.filter((key) => key.required !== "derived");
    if (lines.length) {
      lines.push({ text: "" });
    }
    lines.push({ text: `# ── ${table.title} `.padEnd(60, "─") }, { text: `[${table.table}]` });
    const settings = keys.map((key) => {
      const name = key.key.slice(table.table.length + 1);
      return "default" in key ? `${name} = ${tomlValue(key.default)}` : `# ${name} = ${tomlValue(key.example)}`;
    });
    const column = Math.max(0, ...settings.map((setting) => setting.length).filter((n) => n <= CONFIG_COLUMN)) + 2;
    keys.forEach((key, index) => {
      const comments = commentLines(key.about, CONFIG_RIGHT - column);
      const setting = settings[index];
      if (setting.length + 2 > column) {
        lines.push({ text: setting, key: key.key, note: setting.length });
      } else {
        lines.push({ text: setting.padEnd(column) + comments.shift(), key: key.key, note: column });
      }
      lines.push(...comments.map((text) => ({ text: " ".repeat(column) + text })));
    });
  }
  return lines;
}

/** What the configuration page says per key, in the order of the data and without a DOM:
    [{file, table, key, level, levelLabel, required, default, example, options, about, status,
    readers}]. `default` and `example` are TOML, or null without one. `options` lists the values
    the key accepts, [{value, isDefault}], or is null when its values are free. */
export function configEntries(data, packs) {
  const entries = [];
  for (const file of data.files) {
    for (const table of file.tables) {
      for (const key of table.keys) {
        entries.push({
          file: file.file,
          table: table.table,
          key: key.key,
          level: key.level,
          levelLabel: CONFIG_LEVELS[key.level],
          required: key.required,
          default: "default" in key ? tomlValue(key.default) : null,
          example: "example" in key ? tomlValue(key.example) : null,
          options: key.options
            ? key.options.map((value) => ({ value: String(value), isDefault: key.default === value }))
            : null,
          about: key.about,
          status: key.readers.length
            ? "Status: works"
            : "Status: not used yet — config.py accepts it, nothing acts on it.",
          readers: key.readers.map((path) => configReader(path, packs)),
        });
      }
    }
  }
  return entries;
}

/** A link to one reader, with the glyph of its sort, like the links of the reference page. */
function configReaderLink(reader) {
  const link = el("a", `reference-link kind-${KIND_OF_SORT[reader.sort]} form-${reader.sort}`);
  link.href = reader.href;
  const glyph = el("span", "reference-glyph", SORTS[reader.sort].glyph);
  glyph.setAttribute("aria-hidden", "true");
  link.append(glyph, ` ${reader.name}`);
  const row = el("li");
  row.append(link);
  return row;
}

/** A table with a header row and one row per item of `rows`, each a list of cells, that scrolls
    sideways on a narrow screen. */
function configTable(head, rows, className = "config-table") {
  const table = el("table", className);
  const top = el("tr");
  top.append(...head.map((label) => el("th", "", label)));
  const thead = el("thead");
  thead.append(top);
  const tbody = el("tbody");
  for (const cells of rows) {
    const row = el("tr");
    row.append(...cells);
    tbody.append(row);
  }
  table.append(thead, tbody);
  const wrap = el("div", "config-scroll");
  wrap.append(table);
  return wrap;
}

/** A table cell holding `nodes`. */
function cell(...nodes) {
  const node = el("td");
  node.append(...nodes);
  return node;
}

/** The TOML of one file as a code block, each key a link to its row below. */
function configBlock(file) {
  const code = el("code");
  for (const line of configToml(file)) {
    if (line.key) {
      const name = line.key.split(".").pop();
      const setting = line.text.slice(0, line.note);
      const at = setting.indexOf(name);
      const link = el("a", "", name);
      link.href = `#${line.key}`;
      const comment = el("span", "config-comment", line.text.slice(line.note));
      code.append(setting.slice(0, at), link, setting.slice(at + name.length), comment, "\n");
    } else if (line.text.trimStart().startsWith("#")) {
      code.append(el("span", "config-comment", line.text), "\n");
    } else {
      code.append(`${line.text}\n`);
    }
  }
  const pre = el("pre", "config-toml");
  pre.append(code);
  return pre;
}

/** The value cell of one key: the values it accepts with the default marked, else its default,
    else "not set" with an example. */
function configValue(entry) {
  const node = el("td");
  if (entry.options) {
    const list = el("ul", "config-options");
    for (const option of entry.options) {
      const item = el("li", option.isDefault ? "config-option is-default" : "config-option");
      item.append(el("code", "", option.value));
      if (option.isDefault) {
        item.append(el("span", "config-default-mark", "default"));
      }
      list.append(item);
    }
    node.append(list);
    if (entry.default === null) {
      node.append(el("p", "config-none", "not set"));
    }
  } else if (entry.default !== null) {
    node.append(el("code", "", entry.default));
  } else {
    node.append(el("span", "config-none", "not set"));
    if (entry.example !== null) {
      const example = el("p", "config-none", "e.g. ");
      example.append(el("code", "", entry.example));
      node.append(example);
    }
  }
  return node;
}

/** The row of one key: its name and level, its value, what it does and what reads it. */
function configRow(entry) {
  const name = el("td");
  name.id = entry.key;
  const level = el("span", `config-level level-${entry.level}`, entry.levelLabel);
  name.append(el("code", "", entry.key), " ", level);
  if (entry.required !== "no") {
    const required = {
      yes: "Required",
      derived: "Derived from forge.repo; you do not set it.",
    }[entry.required] || `Required ${entry.required}`;
    name.append(el("p", "config-none", required));
  }
  let readers;
  if (entry.readers.length) {
    readers = el("ul", "reference-list");
    readers.append(...entry.readers.map(configReaderLink));
  } else {
    readers = el("p", "config-none", "Nothing reads it yet: config.py only checks it.");
  }
  return [name, configValue(entry), cell(el("p", "", entry.about)), cell(readers)];
}

/** The configuration page: where the two files are, each file as TOML with its defaults and a
    comment behind every key, and per table its title, what it is for and one row per key. */
export function renderConfig(root, data, packs) {
  const entries = configEntries(data, packs);
  const paths = el("section", "config-file");
  const places = data.files.map((file) => {
    const where = el("td");
    where.append(el("code", "", file.path), el("p", "config-none", `Elsewhere: set ${file.override}.`));
    return [cell(el("code", "", file.file)), where, cell(el("p", "", file.written))];
  });
  paths.append(el("h2", "", "Where the files are"), configTable(["File", "Path", "Who writes it"], places, "config-table config-paths"));
  const sections = [paths];
  for (const file of data.files) {
    const section = el("section", "config-file");
    const note =
      "Every key with its default and what it does. A key without a default is commented out, with an example.";
    section.append(el("h2", "", file.file), el("p", "", note), configBlock(file));
    sections.push(section);
  }
  for (const file of data.files) {
    const section = el("section", "config-file");
    section.append(el("h2", "", `The keys of ${file.file}`));
    for (const table of file.tables) {
      const rows = entries
        .filter((entry) => entry.file === file.file && entry.table === table.table)
        .map(configRow);
      const head = ["Key", "Value", "What it does", "Read by"];
      const title = el("h3", "", `${table.title} `);
      title.append(el("code", "", `[${table.table}]`));
      const theme = el("div", "config-theme");
      theme.id = table.table;
      theme.append(title, el("p", "", table.about), configTable(head, rows));
      section.append(theme);
    }
    sections.push(section);
  }
  root.replaceChildren(...sections);
}

/** The three positions of the switch above the tree of a repo after init. */
export const REPO_MODES = [
  { id: "everything", label: "Everything" },
  { id: "adds", label: "Adds" },
  { id: "edits", label: "Edits your file" },
];
const REPO_ACTS = { adds: "init adds it", edits: "init edits your file" };

/** The tree without a DOM: the dirs in order, then the workstation, each with its rows.
    [{id, label, yours, rows: [{id, place, label, kind, glyph, className, act, actLabel, href, dim}]},
    …, {id: "workstation", label, note, rows}]. A row dims when `mode` shows the other act; the
    yours of a dir never dims. */
export function repoEntries(data, mode = "everything") {
  if (!REPO_MODES.some((m) => m.id === mode)) {
    throw new Error(`unknown mode: ${mode}`);
  }
  const rowsOf = (dir) =>
    data.rows
      .filter((row) => row.dir === dir)
      .map((row) => {
        if (!SORTS[row.kind]) {
          throw new Error(`unknown kind: ${row.kind}`);
        }
        if (!REPO_ACTS[row.act]) {
          throw new Error(`unknown act: ${row.act}`);
        }
        return {
          id: row.id,
          place: row.place || null,
          label: row.label,
          kind: row.kind,
          glyph: SORTS[row.kind].glyph,
          className: `kind-${KIND_OF_SORT[row.kind] || row.kind} form-${row.kind}`,
          act: row.act,
          actLabel: REPO_ACTS[row.act],
          href: row.href || null,
          dim: mode !== "everything" && row.act !== mode,
        };
      });
  const dirs = data.dirs.map((dir) => ({
    id: dir.id,
    label: dir.label,
    yours: dir.id === "root" ? data.yours : dir.yours || null,
    rows: rowsOf(dir.id),
  }));
  const station = { id: "workstation", ...data.workstation, rows: rowsOf("workstation") };
  return [...dirs, station];
}

/** One row of the tree: its glyph, its label, what init does with it, and a ↗ to where it is
    explained. The ↗ has no text of its own; its name comes from the row. */
function repoRow(row) {
  const item = el("li");
  const pill = el("span", `repo-row ${row.className}`);
  pill.dataset.id = row.id;
  const glyph = el("span", "glyph", row.glyph);
  glyph.setAttribute("aria-hidden", "true");
  pill.append(glyph, el("code", "", row.label), el("span", "sr-only", `, ${row.actLabel}`));
  if (row.href) {
    const go = el("a", "repo-go", "↗");
    go.href = row.href;
    go.setAttribute("aria-label", `More about ${row.label}`);
    pill.append(go);
  }
  item.append(pill);
  return item;
}

function repoYours(text) {
  const item = el("li");
  item.append(el("span", "repo-yours", text));
  return item;
}

/** The tree of a repo after init: the switch, your repo as an indented tree, and the workstation
    in a box of its own. */
export function renderRepo(root, data) {
  const [top, ...rest] = repoEntries(data);
  const dirs = rest.filter((entry) => entry.id !== "workstation");
  const station = rest.find((entry) => entry.id === "workstation");

  const bar = el("div", "routes repo-switch");
  bar.setAttribute("role", "group");
  bar.setAttribute("aria-label", "What init does");
  const buttons = REPO_MODES.map((mode) => {
    const button = el("button", "", mode.label);
    button.type = "button";
    button.dataset.mode = mode.id;
    button.setAttribute("aria-pressed", String(mode.id === "everything"));
    return button;
  });
  bar.append(...buttons);

  const tree = el("div", "repo-tree");
  const list = el("ul");
  list.append(...top.rows.map(repoRow));
  for (const dir of dirs) {
    const item = el("li");
    const sub = el("ul");
    sub.append(...dir.rows.map(repoRow));
    if (dir.yours) {
      sub.append(repoYours(dir.yours));
    }
    item.append(el("span", "repo-dir", dir.label), sub);
    list.append(item);
  }
  list.append(repoYours(top.yours));
  tree.append(el("h2", "", top.label), list);

  const stationBox = el("section", "repo-station");
  const stationList = el("ul");
  stationList.append(...station.rows.map(repoRow));
  stationBox.append(el("h2", "", station.label), stationList, el("p", "repo-note", station.note));

  const select = (mode) => {
    buttons.forEach((button) => button.setAttribute("aria-pressed", String(button.dataset.mode === mode)));
    const dim = new Map(repoEntries(data, mode).flatMap((entry) => entry.rows.map((row) => [row.id, row.dim])));
    for (const node of root.querySelectorAll(".repo-row")) {
      node.classList.toggle("dim", dim.get(node.dataset.id));
    }
  };
  buttons.forEach((button) => button.addEventListener("click", () => select(button.dataset.mode)));

  root.replaceChildren(bar, tree, stationBox);
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
    const title = el("h2", step.command ? "story-title story-command-line" : "story-title");
    if (step.command) {
      // The place of a command goes before it, as in a prompt: "in repo: $ lighthouse init".
      if (step.where) {
        title.append(el("span", "story-place", `${step.where}:`));
      }
      title.append(el("code", "story-command", `$ ${step.command}`));
    } else {
      title.textContent = step.title;
    }
    node.append(title);
    if (step.where && !step.command) {
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
    if (step.map) {
      const link = el("a", "story-map", step.map.label);
      link.href = step.map.href;
      node.append(link);
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

/** The index of the route to show for an item made of `ids` (one chip, or the two ends of a
    handoff): `current` when that route passes them in this order, else the first route that
    does, else `current`. */
export function routeWith(routes, current, ids) {
  const passes = (route) => {
    const at = ids.map((id) => route.chips.indexOf(id));
    return at.every((i, n) => i >= 0 && (n === 0 || at[n - 1] <= i));
  };
  if (routes[current] && passes(routes[current])) {
    return current;
  }
  const found = routes.findIndex(passes);
  return found >= 0 ? found : current;
}

/**
 * Renders a flow map into `root`: route buttons with the note of the chosen route, columns of
 * chips and the handoff badges. A click on a chip or badge opens `detail` as a drawer from the
 * right, and switches to a route it is on when the chosen route passes it by; Escape or its
 * close button shuts the drawer.
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
    "Click a chip or a handoff for what it does, when it starts and who provides it. Off the chosen route, the click switches to a route it is on.",
  );
  // Called after the drawer is filled: select redraws it with the neighbours on the new route.
  const follow = (ids) => {
    const index = routeWith(routes, routes.indexOf(chosen), ids);
    if (index >= 0 && routes[index] !== chosen) {
      select(index);
    }
  };

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
    badge.addEventListener("click", () => {
      showHandoff(handoff, badge);
      follow([handoff.from, handoff.to]);
    });
    badges.push(badge);
    return badge;
  };

  for (const chip of placed) {
    const button = chipButton(chip, phases.length);
    const p = point(chip);
    button.style.left = percent(p.x);
    button.style.top = `${p.y}px`;
    button.addEventListener("click", () => {
      showChip(chip, button);
      follow([chip.id]);
    });
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
