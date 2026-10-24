# lighthouse: core@dev
"""Parser voor `git commit`/`git push` binnen een willekeurig shell-commando.

Verhuisd uit een `IFS= read -r -d '' PARSE <<'PYEOF'`-heredoc in
main-commit-guard.sh (#884): die vorm bestond alleen om bash 3.2 (macOS) niet
te laten struikelen over een heredoc BINNEN een command substitution. Als los
bestand is die reden vervallen en kan de parser gewoon los getest worden.

Gebruikt door main-commit-guard.sh (PreToolUse-hook die `git commit`/`git
push` op `main` blokkeert) en, via dezelfde `find_invocations`, door de
pre-commit test-gate.
"""

# De hooks draaien onder `python3` van de host (macOS: 3.9.6); zonder deze
# import is `list[str] | None` in `main` een TypeError bij de import.
from __future__ import annotations

import os
import sys
from collections.abc import Iterator
from typing import NamedTuple

UNRESOLVED = "<unknown>"
NEW_DIR = "<new>"

# Grenspalen van een sub-shell in de segmentenlijst. Identiteit telt, niet
# inhoud: flush_segment() maakt altijd een verse lijst, dus verwarring kan niet.
SUB_ENTER = ["<enter-subshell>"]
SUB_LEAVE = ["<leave-subshell>"]

# Scheidingsteken `&&`: wat erna komt draait ALLEEN als het vorige slaagde.
# Bij `;`, `|`, `||` en een newline draait het volgende hoe dan ook. Dat
# verschil weegt bij een `cd` naar een map die er nog niet is (F3).
AND_SEP = ["<and-sep>"]
MARKERS = (SUB_ENTER, SUB_LEAVE, AND_SEP)

# Woorden die vóór het eigenlijke commando mogen staan zonder het te zijn.
# Per wrapper: (opties die een lósse waarde slikken, aantal positionele
# argumenten vóór het commando). `timeout 5 git commit` heeft er zo één —
# alleen het wrapperwoord overslaan liet die commit erdoor (N7).
WRAPPERS = {
    "sudo": ({"-u", "-g", "-p", "-C", "--user", "--group", "--prompt"}, 0),
    "doas": ({"-u", "-C"}, 0),
    "command": (set(), 0),
    "nice": ({"-n", "--adjustment"}, 0),
    "time": (set(), 0),
    "env": ({"-u", "--unset", "-C", "--chdir"}, 0),
    "rtk": (set(), 0),
    "exec": ({"-a"}, 0),
    "nohup": (set(), 0),
    "stdbuf": ({"-i", "-o", "-e"}, 0),
    "timeout": ({"-k", "--kill-after", "-s", "--signal"}, 1),
    "xargs": (
        {"-I", "-n", "-P", "-s", "-d", "-E", "-a", "--replace", "--max-args"},
        0,
    ),
}
# Schillen die een commando als string meekrijgen: `bash -c <script>`. Dat
# script is een echt commando en wordt apart geparsed.
SHELL_WRAPPERS = {"bash", "sh", "zsh", "dash", "ksh"}
SKIP_WORDS = {
    "{",
    "}",
    "!",
    "if",
    "then",
    "else",
    "elif",
    "fi",
    "while",
    "until",
    "do",
    "done",
    "for",
    "case",
    "esac",
}
GLOBAL_WITH_VALUE = {
    "-C",
    "-c",
    "--git-dir",
    "--work-tree",
    "--namespace",
    "--exec-path",
}
PATH_OPTIONS = {"-C", "--git-dir", "--work-tree"}


class Unparseable(Exception):
    """Open quote: er valt geen conclusie te trekken over dit commando."""


def read_delimiter(line, j):
    """Lees de heredoc-delimiter vanaf positie j. Geeft (delimiter, nieuwe j)."""
    n = len(line)
    out = []
    while j < n:
        c = line[j]
        if c == "\\":
            j += 1
            if j < n:
                out.append(line[j])
                j += 1
            continue
        if c in "'\"":
            quote = c
            j += 1
            while j < n and line[j] != quote:
                out.append(line[j])
                j += 1
            if j < n:
                j += 1
            continue
        if c.isspace() or c in ";|&<>()":
            break
        out.append(c)
        j += 1
    return "".join(out), j


def skip_ansi_c(text, j):
    """Lees een `$'...'`-string vanaf j, de positie ná de openingsquote.

    Geeft (inhoud, index ná de sluitquote, gesloten). Binnen ANSI-C quoting
    escapet een backslash het volgende teken, óók een apostrof — dat is precies
    het verschil met een gewone single quote.
    """
    out = []
    n = len(text)
    while j < n:
        if text[j] == "\\" and j + 1 < n:
            out.append(text[j + 1])
            j += 2
            continue
        if text[j] == "'":
            return "".join(out), j + 1, True
        out.append(text[j])
        j += 1
    return "".join(out), j, False


def quoted_step(line, i, sq, dq):
    """Eén stap binnen `'...'` of `"..."`. Geeft (i, sq, dq)."""
    c = line[i]
    if sq:
        if c == "'":
            sq = False
        return i + 1, sq, dq
    if c == "\\" and i + 1 < len(line):
        return i + 2, sq, dq
    if c == '"':
        dq = False
    return i + 1, sq, dq


def heredoc_operator(line, i, arith):
    """Lees een `<<` vanaf i. Geeft (i, word_start, delimiter), "" zonder delimiter."""
    if line.startswith("<<<", i):
        return i + 3, True, ""
    if arith > 0:
        # `$((1 << 2))` is een bitshift, geen heredoc.
        return i + 2, False, ""
    n = len(line)
    j = i + 2
    if j < n and line[j] == "-":
        j += 1
    while j < n and line[j] in " \t":
        j += 1
    delimiter, j = read_delimiter(line, j)
    return j, True, delimiter


def unquoted_step(line, i, arith):
    """Eén stap buiten quotes. Geeft (i, arith, sq, dq, word_start)."""
    c = line[i]
    if c == "\\":
        return i + 2, arith, False, False, False
    if c == "'":
        return i + 1, arith, True, False, False
    if c == '"':
        return i + 1, arith, False, True, False
    if c == "$" and line.startswith("((", i + 1):
        return i + 3, arith + 1, False, False, False
    if c == "$" and line.startswith("'", i + 1):
        # ANSI-C quoting: in `$'...'` sluit een backslash-escaped apostrof
        # de string NIET. Zonder deze tak las de scanner `$'it\'s'` als een
        # gewone quote, sloot hem op de escaped apostrof en liet de laatste
        # openstaan (F4).
        return skip_ansi_c(line, i + 2)[1], arith, False, False, False
    if arith > 0 and line.startswith("))", i):
        return i + 2, arith - 1, False, False, False
    return i + 1, arith, False, False, c in " \t;&|()<>"


def scan_line(line, sq, dq):
    """Heredoc-operatoren in redirectiepositie plus het begin van `#`-commentaar.

    Geeft (delimiters, sq, dq, snijpunt). Het snijpunt is de index waar een
    commentaar begint, of len(line). Bash begint een commentaar alleen bij een
    `#` aan het BEGIN van een woord en buiten quotes: `foo#bar`, `$#` en
    `'a # b'` zijn dus geen commentaar. Dat onderscheid is precies waarom dit
    hier hoort en niet in een losse regex: zonder quote-stand is het niet te
    zien, en één losse apostrof in een commentaar blokkeerde anders élk
    commando (N1) of slikte een `<<` in een commentaar de rest op (N2).
    """
    delimiters = []
    arith = 0
    word_start = not (sq or dq)
    i, n = 0, len(line)
    while i < n:
        if sq or dq:
            i, sq, dq = quoted_step(line, i, sq, dq)
            continue
        if line[i] == "#" and word_start:
            return delimiters, sq, dq, i
        if line.startswith("<<", i):
            i, word_start, delimiter = heredoc_operator(line, i, arith)
            if delimiter:
                delimiters.append(delimiter)
            continue
        i, arith, sq, dq, word_start = unquoted_step(line, i, arith)
    return delimiters, sq, dq, n


def strip_heredocs(command):
    """Heredoc-bodies en `#`-commentaar eruit.

    Een voorbeeldcommando IN een heredoc is geen commando — dit is precies waar
    pre-commit-tests.sh op struikelt. De delimiter wordt echt geparsed (ook
    `<<-`, `<<'EOF-1'`, `<<"EOF"`), en `<<` telt alleen als heredoc wanneer het
    in redirectiepositie staat — niet in een string, niet in `$(( ))`, niet in
    een commentaar.

    Het commentaar sneuvelt hier omdat scan_line de quote-stand toch al
    bijhoudt: één scanner, twee klussen. Wat overblijft gaat schoon de
    tokenizer in, zodat een apostrof in een commentaar daar niets meer opent.
    """
    kept = []
    queue = []
    sq = dq = False
    for line in command.split("\n"):
        if queue:
            if line.strip() == queue[0]:
                queue.pop(0)
            continue
        delimiters, sq, dq, cut = scan_line(line, sq, dq)
        kept.append(line[:cut])
        queue = delimiters
    return "\n".join(kept)


class Splitter:
    """Toestandsmachine achter split_segments.

    Per stap één teken, of één geheel stuk: een quote, een escape, `&&`, een
    `<>`-reeks of een `$(`-opening.
    """

    def __init__(self, text):
        self.text = text
        self.n = len(text)
        self.segments = []
        self.outer = []
        self.stack = []
        self.current = []
        self.buf = []
        self.dirty = False
        self.started = False
        self.tilde = False

    def flush_word(self):
        if self.started or self.buf:
            self.current.append(("".join(self.buf), not self.dirty, self.tilde))
        self.buf = []
        self.dirty = False
        self.started = False
        self.tilde = False

    def flush_segment(self):
        self.flush_word()
        self.segments.append(self.current)
        self.current = []

    def enter_sub(self, kind):
        self.outer.append(
            (self.current, self.buf, self.dirty, self.started, self.tilde)
        )
        self.current = []
        self.buf = []
        self.dirty = False
        self.started = False
        self.tilde = False
        self.stack.append(kind)
        self.segments.append(SUB_ENTER)

    def leave_sub(self, substitution=True):
        self.flush_segment()
        self.segments.append(SUB_LEAVE)
        current, buf, dirty, started, tilde = self.outer.pop()
        self.current = current
        if substitution:
            # De uitkomst van een substitutie is niet te herleiden.
            self.buf = buf + ["$"]
            self.dirty = True
            self.started = True
            self.tilde = False
        else:
            # `( )` is een groep, geen woord: het omliggende woord blijft heel.
            self.buf = buf
            self.dirty = dirty
            self.started = started
            self.tilde = tilde
        self.stack.pop()

    def dq_char(self, i):
        """Eén teken binnen `"..."`. Geeft de nieuwe i."""
        text, n, c = self.text, self.n, self.text[i]
        if c == "\\" and i + 1 < n and text[i + 1] in '$`"\\\n':
            if text[i + 1] != "\n":
                self.buf.append(text[i + 1])
                self.started = True
            return i + 2
        if c == '"':
            self.stack.pop()
            return i + 1
        if c == "$" and i + 1 < n and text[i + 1] == "(":
            self.enter_sub("sub")
            return i + 2
        if c == "`":
            self.enter_sub("bq")
            return i + 1
        if c == "$":
            self.dirty = True
        self.buf.append(c)
        self.started = True
        return i + 1

    def quote_char(self, i):
        """`$'`, `'`, `"` of `\\` op i. Geeft de nieuwe i, of None."""
        text, n, c = self.text, self.n, self.text[i]
        if c == "$" and i + 1 < n and text[i + 1] == "'":
            body, i, closed = skip_ansi_c(text, i + 2)
            if not closed:
                raise Unparseable()
            # De escapes worden hier niet uitgerold (`\n` blijft `n`), dus het
            # woord geldt als niet-letterlijk: goed genoeg om een pad te
            # herkennen, te onzeker om er een bestemming op te baseren.
            self.buf.append(body)
            self.dirty = True
            self.started = True
            return i
        if c == "'":
            end = text.find("'", i + 1)
            if end < 0:
                raise Unparseable()
            self.buf.append(text[i + 1 : end])
            self.started = True
            return end + 1
        if c == '"':
            self.stack.append("dq")
            self.started = True
            return i + 1
        if c == "\\":
            if i + 1 < n and text[i + 1] == "\n":
                # Regelvoortzetting: de twee regels zijn één commando.
                return i + 2
            if i + 1 < n:
                self.buf.append(text[i + 1])
                self.started = True
                return i + 2
            return i + 1
        return None

    def subshell_char(self, i, top):
        """`$(`, `)`, backtick of `(` op i. Geeft de nieuwe i, of None."""
        text, n, c = self.text, self.n, self.text[i]
        if c == "$" and i + 1 < n and text[i + 1] == "(":
            self.enter_sub("sub")
            return i + 2
        if c == ")" and top in ("sub", "paren"):
            self.leave_sub(top == "sub")
            return i + 1
        if c == "`":
            if top == "bq":
                self.leave_sub()
            else:
                self.enter_sub("bq")
            return i + 1
        if c == "(":
            self.flush_segment()
            self.enter_sub("paren")
            return i + 1
        return None

    def separator_char(self, i):
        """Witruimte, `&&`, een segmentgrens of `<>` op i. Geeft de nieuwe i, of None."""
        text, n, c = self.text, self.n, self.text[i]
        if c in " \t\r":
            self.flush_word()
            return i + 1
        if c == "&" and i + 1 < n and text[i + 1] == "&":
            self.flush_segment()
            self.segments.append(AND_SEP)
            return i + 2
        if c == "\n" or c in ";&|)":
            self.flush_segment()
            return i + 1
        if c in "<>":
            self.flush_word()
            start = i
            while i < n and text[i] in "<>":
                i += 1
            self.current.append((text[start:i], True, False))
            return i
        return None

    def word_char(self, i):
        """Een gewoon woordteken op i. Geeft de nieuwe i."""
        c = self.text[i]
        if c in "$*?":
            self.dirty = True
        elif c == "~" and not self.started and not self.buf:
            self.tilde = True
        self.buf.append(c)
        self.started = True
        return i + 1

    def run(self):
        i = 0
        while i < self.n:
            top = self.stack[-1] if self.stack else None
            if top == "dq":
                i = self.dq_char(i)
                continue
            step = self.quote_char(i)
            if step is None:
                step = self.subshell_char(i, top)
            if step is None:
                step = self.separator_char(i)
            i = self.word_char(i) if step is None else step

        while self.stack:
            if self.stack[-1] == "dq":
                raise Unparseable()
            self.leave_sub(self.stack[-1] != "paren")
        self.flush_segment()
        return self.segments


def split_segments(text):
    """Splits in commandosegmenten. Per woord onthouden we hoe het ontstond.

    Een nieuw segment begint bij een newline, `;`, `&`, `|`, een sub-shell `(`,
    een command substitution `$(` of een backtick — precies de plekken waar een
    nieuw commando begint. Redirecties splitsen alleen het woord.

    Een substitutie levert twee dingen op, en dat is bewust: de inhoud wordt als
    eigen segment onderzocht (`echo "$(git commit ...)"` commit écht), en het
    omliggende woord wordt als niet-herleidbaar gemarkeerd (`git -C "$(pwd)"`
    kan niet stil op de cwd terugvallen).

    Rond elke sub-shell — `( )`, `$( )`, backticks — komen SUB_ENTER/SUB_LEAVE
    in de lijst. Bash geeft de cwd van een sub-shell niet terug aan de ouder;
    zonder die haakjes bleef een `cd` erbinnen hangen en blokkeerde de hook een
    volstrekt legitieme commit erna (N3).

    Per woord komt er een tripel (tekst, letterlijk, tilde): `tilde` zegt of het
    woord met een ONgequote `~` begon, want alleen dan expandeert bash hem.
    """
    return Splitter(text).run()


def resolve(word, literal, tilde):
    """Herleid een padwoord. None = niet met zekerheid te herleiden.

    De tilde expandeert alleen als hij ONgequote was: `'~/repo'` is voor bash
    een gewoon relatief pad dat met een tilde begint (N8).
    """
    text = word if literal else os.path.expandvars(word)
    if tilde:
        text = os.path.expanduser(text)
    if any(ch in text for ch in "$`*?"):
        return None
    return text


def basename(word):
    return word.rsplit("/", 1)[-1] if "/" in word else word


def is_assignment(word):
    name, sep, _ = word.partition("=")
    if not sep or not name:
        return False
    if not (name[0].isalpha() or name[0] == "_"):
        return False
    return all(ch.isalnum() or ch == "_" for ch in name)


def command_start(words):
    """Sla toewijzingen, wrappers en shell-sleutelwoorden over."""
    idx = 0
    moved = True
    while moved and idx < len(words):
        moved = False
        while idx < len(words) and is_assignment(words[idx]):
            idx += 1
            moved = True
        while idx < len(words) and words[idx] in SKIP_WORDS:
            idx += 1
            moved = True
        if idx < len(words) and basename(words[idx]) in WRAPPERS:
            with_value, positional = WRAPPERS[basename(words[idx])]
            idx += 1
            while idx < len(words) and words[idx].startswith("-") and words[idx] != "-":
                if "=" not in words[idx] and words[idx] in with_value:
                    idx += 2
                else:
                    idx += 1
            for _ in range(positional):
                if idx < len(words) and not words[idx].startswith("-"):
                    idx += 1
            moved = True
    return idx


def refspec_target(arg):
    spec = arg.removeprefix("+")
    if ":" in spec:
        spec = spec.split(":", 1)[1]
    spec = spec.removeprefix("refs/heads/")
    return spec


def push_destination(words, literal, default_branch="main"):
    """Bestemming van een push: yes (`default_branch`), all (alle branches), no,
    unresolved.

    `unresolved` is de vierde uitkomst en hij blokkeert. `git push origin $BR`
    kán main zijn en de hook kan het niet zien; stil doorlaten zou hier hetzelfde
    gat zijn dat een onherleidbaar `-C`-pad al niet mag maken (N4).

    Het EERSTE positionele argument is de remote, niet de bestemming. Een
    onherleidbare remote zegt niets over waar de push heen gaat: bij
    `git push "$REMOTE" issue-1-iets` is de branch gewoon bekend en niet main.
    Die mee laten tellen maakte van een bekende bestemming een blokkade (F2).
    """
    result = "no"
    positional = 0
    for k, arg in enumerate(words):
        if arg[:1] in ("<", ">"):
            break
        if arg in ("--all", "--mirror"):
            result = "all"
            continue
        if arg.startswith("-"):
            continue
        positional += 1
        if not literal[k]:
            if result == "no" and positional > 1:
                result = "unresolved"
            continue
        if refspec_target(arg) == default_branch:
            return "yes"
    return result


def existing_ancestor(path):
    """De dichtstbijzijnde map die NU bestaat, vanaf `path` omhoog. None = geen."""
    current = path
    while True:
        if os.path.isdir(current):
            return current
        parent = os.path.dirname(current)
        if parent == current:
            return None
        current = parent


def repo_of(path):
    """Repowortel waarin `path` ligt, of None.

    Zonder git aan te roepen: omhoog lopen tot er een `.git` staat. Een worktree
    heeft daar een bestand in plaats van een map, dus `exists` en niet `isdir`.
    De uitkomst hoeft niet exact te zijn — hij bepaalt alleen of het pad aan git
    wordt voorgelegd; git zelf velt daarna het echte oordeel.

    Eén ding slaat git zelf al over, en wij dus ook: een `.git`-MAP zonder
    `HEAD`. Dat is geen (kapotte) repo maar een verdwaalde map — op de Mac
    `~/.git/workflow-audit-tools/` — en git's discovery loopt erlangs omhoog.
    Telde hij hier mee, dan werd elke map onder `~` een wortel waar git "not a
    git repository" op zegt. Een `.git/HEAD` met rommel blijft wél een wortel:
    dat is een repo die git kapot verklaart, en dat oordeel hoort luid.
    """
    current = path
    while True:
        dot_git = os.path.join(current, ".git")
        if os.path.isfile(dot_git) or os.path.lexists(os.path.join(dot_git, "HEAD")):
            return current
        parent = os.path.dirname(current)
        if parent == current:
            return None
        current = parent


def new_dirs(cwd):
    """Mapstand van de shell: waar we staan, waar we vandaan komen, de stack."""
    return {
        "here": cwd,
        "raw": cwd,
        "old": None,
        "old_raw": None,
        "old_set": False,
        "stack": [],
        "new_dir": False,
        "stale": None,
    }


def apply_cd(dirs, here, raw):
    dirs["old"], dirs["old_raw"], dirs["old_set"] = dirs["here"], dirs["raw"], True
    dirs["here"], dirs["raw"] = here, raw
    dirs["new_dir"] = False


def stack_move(verb, words, args, dirs):
    """`popd`, `cd -` en `pushd`; True als de verplaatsing daarmee af is.

    Bij `pushd` mét argument zet hij alleen `(here, raw)` op de stack en geeft
    hij False: de eigenlijke verplaatsing doet `cd_to`.
    """
    if verb == "popd":
        if dirs["stack"]:
            previous, previous_raw = dirs["stack"].pop()
            apply_cd(dirs, previous, previous_raw)
        else:
            apply_cd(dirs, None, "popd")
        return True
    if args and words[args[0]] == "-":
        if dirs["old_set"]:
            apply_cd(dirs, dirs["old"], dirs["old_raw"])
        else:
            # De OLDPWD van vóór dit commando kennen we niet.
            apply_cd(dirs, None, "cd -")
        return True
    if verb == "pushd":
        if not args:
            # `pushd` zonder argument wisselt de bovenste twee: niet te volgen.
            apply_cd(dirs, None, "pushd")
            return True
        dirs["stack"].append((dirs["here"], dirs["raw"]))
    return False


def cd_to(word, literal, tilde, dirs):
    """Verplaats naar `word`: oplossen, samenvoegen met `here`, en de verse map."""
    target = resolve(word, literal, tilde)
    if target is None:
        apply_cd(dirs, None, word)
        return
    if os.path.isabs(target):
        target = os.path.normpath(target)
    elif dirs["here"] is not None:
        target = os.path.normpath(os.path.join(dirs["here"], target))
    else:
        dirs["raw"] = word
        return
    if not os.path.isdir(target):
        # De map bestaat NU niet, en dat heeft twee tegengestelde uitkomsten.
        # Ofwel het commando maakt hem zelf aan — `mkdir -p X && cd X`,
        # `git worktree add X && cd X`, de motie die AGENTS.md voorschrijft —
        # en bash staat straks in die verse map. Ofwel de `cd` mislukt en bash
        # blijft staan. Bij `&&` draait wat volgt alleen in het eerste geval:
        # we rekenen dan verder mét dit nog niet bestaande pad, zodat een `cd ..`
        # of `git -C ..` erna gewoon klopt. Welke repo het is, beslist settle_target()
        # via de dichtstbijzijnde bestaande voorouder (C1) — een verse map ONDER
        # een checkout is diezelfde checkout, met diezelfde branch. Bij `;`
        # draait de rest hoe dan ook hier; settle_new_dir() zet dan de oude map
        # terug, want daar staat bash dan echt.
        stale = (dirs["here"], dirs["raw"])
        apply_cd(dirs, target, target)
        dirs["new_dir"], dirs["stale"] = True, stale
        return
    apply_cd(dirs, target, target)


def change_dir(verb, words, literal, tildes, idx, dirs):
    """Volg `cd`, `pushd` en `popd` zoals bash ze uitvoert.

    Drie dingen die de vorige versie miste (N6): een `cd` naar een pad dat niet
    bestaat verplaatst bash niet, `cd -` springt terug naar de vorige map, en
    `pushd`/`popd` verplaatsen net zo goed als `cd`.
    """
    args = [
        k
        for k in range(idx + 1, len(words))
        if not (words[k].startswith("-") and len(words[k]) > 1)
    ]
    if stack_move(verb, words, args, dirs):
        return
    if not args:
        apply_cd(dirs, os.path.expanduser("~"), "~")
        return
    k = args[0]
    cd_to(words[k], literal[k], tildes[k], dirs)


def shell_script(words, literal, idx):
    """De letterlijke `-c`-string van een schil, of None."""
    j = idx + 1
    while j < len(words):
        word = words[j]
        if not word.startswith("-") or word == "-":
            return None
        flags = word[1:] if not word.startswith("--") else ""
        if word == "-c" or "c" in flags:
            if j + 1 < len(words) and literal[j + 1]:
                return words[j + 1]
            return None
        j += 1
    return None


def is_name(word):
    """Ziet dit woord eruit als een functienaam?"""
    if not word or word[0].isdigit():
        return False
    return all(ch.isalnum() or ch in "_-." for ch in word)


def body_end(queue):
    """Index net ná het `{ ... }`-blok waarmee een functiebody begint."""
    depth = 0
    for k, item in enumerate(queue):
        if any(item is marker for marker in MARKERS):
            continue
        for word, _, _ in item:
            if word == "{":
                depth += 1
            elif word == "}":
                depth -= 1
        if depth <= 0:
            return k + 1
    return len(queue)


def function_body(words, queue):
    """Is dit een `foo() { ... }`-definitie? Zo ja: isoleer de body.

    De body draait pas bij een AANROEP, dus een `cd` erin verplaatst de
    aanroeper niet — die volgen alsof hij uitgevoerd werd, blokkeerde een
    volstrekt legitieme commit erna (F5). De commando's in de body blijven wél
    zichtbaar, want `f() { git commit -m x; }; f` commit écht; alleen de
    mapstand wordt geïsoleerd, precies zoals bij een sub-shell.
    """
    if not (len(queue) >= 3 and queue[0] is SUB_ENTER and queue[2] is SUB_LEAVE):
        return False
    if queue[1]:
        return False
    if not is_name(words[-1]):
        return False
    if len(words) > 2 or (len(words) == 2 and words[0] != "function"):
        return False
    del queue[:3]
    queue.insert(body_end(queue), SUB_LEAVE)
    queue.insert(0, SUB_ENTER)
    return True


Invocation = tuple[str, str, str, str]


class Step(NamedTuple):
    """Eén segment tijdens de `walk`, met de mapstand die op dat moment geldt.

    `idx` is `command_start(words)`: het woord waar het eigenlijke commando
    begint. `dirs` is de LEVENDE mapstand — hetzelfde dict-object dat `walk`
    intern bijhoudt, niet een kopie. Een consument die de stand van dít
    segment wil bewaren, kopieert zelf wat hij nodig heeft vóórdat hij de
    generator verder vraagt.
    """

    words: list[str]
    literal: list[bool]
    tildes: list[bool]
    idx: int
    dirs: dict


def segment_columns(segment):
    """De drie kolommen van een segment: `(words, literal, tildes)`."""
    words = [w for w, _, _ in segment]
    literal = [lit for _, lit, _ in segment]
    tildes = [til for _, _, til in segment]
    return words, literal, tildes


class WalkState:
    """De staat die `walk` van segment naar segment meedraagt."""

    def __init__(self, queue, cwd):
        self.queue = queue
        self.dirs = new_dirs(cwd)
        self.saved = []
        self.after_and = False

    def take_marker(self, segment):
        """Verwerk `AND_SEP`/`SUB_ENTER`/`SUB_LEAVE`; True als het een marker was."""
        if segment is AND_SEP:
            self.after_and = True
            return True
        if segment is SUB_ENTER:
            self.saved.append(dict(self.dirs, stack=list(self.dirs["stack"])))
            return True
        if segment is SUB_LEAVE:
            if self.saved:
                self.dirs = self.saved.pop()
            return True
        return False

    def settle_new_dir(self):
        """Val terug op de oude map als er geen `&&` voor een verse map stond."""
        if self.dirs["new_dir"] and not self.after_and:
            # Geen `&&` ervoor: dit draait ook als die `cd` mislukte, en dan
            # staat bash nog gewoon in de oude map. Zie cd_to.
            self.dirs["here"], self.dirs["raw"] = self.dirs["stale"]
            self.dirs["new_dir"] = False
        self.after_and = False

    def apply_effect(self, words, literal, tildes, idx):
        """Pas het effect toe van een `cd`/`pushd`/`popd` of een schil (`bash -c`)."""
        verb = basename(words[idx])
        if verb in ("cd", "pushd", "popd"):
            change_dir(verb, words, literal, tildes, idx, self.dirs)
        elif verb in SHELL_WRAPPERS:
            script = shell_script(words, literal, idx)
            if script is not None:
                # Een eigen schil met een eigen cwd: haakjes eromheen.
                inner = split_segments(strip_heredocs(script))
                self.queue[:0] = [SUB_ENTER] + inner + [SUB_LEAVE]


def walk(command: str, cwd: str) -> Iterator[Step]:
    """Loop door elk commandosegment van `command`, uitgevoerd vanuit `cwd`.

    Yield één `Step` per niet-leeg segment met `idx < len(words)` dat geen
    functiedefinitie is — óók voor `cd`/`pushd`/`popd` en een schil
    (`bash -c`). Pas NADAT de consument de generator verder vraagt (de
    volgende `next()`), past `walk` het effect van dát segment toe: een
    `cd`/`pushd`/`popd` verplaatst `dirs`, en een schil met een letterlijke
    `-c`-string recurseert erin. Zo ziet de consument de mapstand zoals die
    gold TOEN het segment draaide, niet erna.

    Sub-shell opslaan/herstellen (`SUB_ENTER`/`SUB_LEAVE`), de `&&`-terugval
    bij een `cd` naar een verse map, en het isoleren van een functiebody
    zitten allemaal hier — dat is precies de staat die per segment moet
    kloppen.

    Gooit `Unparseable` bij een open quote (zie split_segments), bij de
    EERSTE `next()`: `split_segments` draait pas zodra de generator start.
    """
    state = WalkState(split_segments(strip_heredocs(command)), cwd)
    while state.queue:
        segment = state.queue.pop(0)
        if state.take_marker(segment):
            continue
        words, literal, tildes = segment_columns(segment)
        if not words:
            continue
        state.settle_new_dir()
        if function_body(words, state.queue):
            continue
        idx = command_start(words)
        if idx >= len(words):
            continue

        yield Step(words, literal, tildes, idx, state.dirs)

        state.apply_effect(words, literal, tildes, idx)


def git_target(step: Step) -> tuple[str | None, str, bool, int]:
    """De globale-optielus van `git`: `-C`, `--git-dir`, `--work-tree`.

    `step.idx` wijst op het woord `git`. Geeft (target, target_raw,
    from_new_dir, action_idx) — `action_idx` is het woord ná de laatste
    globale optie, dus `action_idx == len(step.words)` als er geen
    subcommando volgt.
    """
    words, literal, tildes, idx, dirs = (
        step.words,
        step.literal,
        step.tildes,
        step.idx,
        step.dirs,
    )
    target, target_raw = dirs["here"], dirs["raw"]
    from_new_dir = dirs["new_dir"]
    idx += 1
    while idx < len(words) and words[idx].startswith("-"):
        name, sep, inline = words[idx].partition("=")
        raw, value_idx = None, None
        if name in PATH_OPTIONS and sep:
            raw, value_idx = inline, idx
            idx += 1
        elif name in GLOBAL_WITH_VALUE and not sep:
            if name in PATH_OPTIONS and idx + 1 < len(words):
                raw, value_idx = words[idx + 1], idx + 1
            idx += 2
        else:
            idx += 1
        if raw is None:
            continue
        resolved = resolve(raw, literal[value_idx], tildes[value_idx])
        if resolved is None:
            target, target_raw, from_new_dir = None, raw, False
        elif os.path.isabs(resolved):
            target, target_raw = os.path.normpath(resolved), raw
        elif target is not None:
            target = os.path.normpath(os.path.join(target, resolved))
            target_raw = target
        else:
            target_raw = raw
    return target, target_raw, from_new_dir, idx


def settle_target(target, from_new_dir, action, to_main, report_new_dirs):
    """Los een niet-bestaande doelmap op; geeft `(target, keep)`.

    `keep=False` betekent: deze aanroep valt buiten de bescherming en wordt
    overgeslagen.
    """
    if target is not None and not os.path.isdir(target):
        # Deze map bestaat bij hook-tijd niet. Dat maakt hem nog geen
        # niemandsland: loop omhoog naar de dichtstbijzijnde map die er wél
        # is. Ligt die in een repo, dan is de verse map een submap van
        # diezelfde repo, met diezelfde branch — `mkdir -p sub && cd sub &&
        # git commit` landt gewoon op main, en dat blokkeert dus (C1).
        # Alleen een voorouder die in geen enkele repo ligt (`/tmp/wt` →
        # `/tmp`) is echt onbeschermbaar.
        ancestor = existing_ancestor(target)
        if ancestor is None or repo_of(ancestor) is None:
            target, from_new_dir = None, True
        elif report_new_dirs and action == "commit":
            # Idem bewust alleen voor `commit`: zie de docstring van
            # find_invocations.
            target = NEW_DIR
        else:
            target = ancestor
    if target is None and from_new_dir and to_main != "yes":
        # De map ontstaat pas tijdens dit commando en ligt in geen enkele
        # repo: er valt hier geen branch te beschermen, en dit houdt de
        # voorgeschreven `git worktree add … && cd … && git commit`-motie
        # werkend. Een push met een LETTERLIJKE main-bestemming heeft die
        # mapkennis niet nodig en telt daarom altijd mee.
        # Bewuste asymmetrie met een onherleidbaar `-C`-pad: dát pad NOEMT
        # een bestaande repo die we niet konden herleiden, en daar blijft
        # 'niet kunnen kijken' een blokkade. Harmoniseer die twee niet (F3).
        if report_new_dirs and action == "commit":
            target = NEW_DIR
        else:
            return target, False
    return target, True


def find_invocations(
    command: str,
    cwd: str,
    *,
    report_new_dirs: bool = False,
    default_branch: str = "main",
) -> list[Invocation]:
    """Vind git commit/push-aanroepen in `command`, uitgevoerd vanuit `cwd`.

    Geeft een lijst met (actie, map-of-marker, naar-main, ruw-pad)-tupels.
    Gooit Unparseable bij een open quote (zie split_segments).

    `report_new_dirs=True` verandert het gedrag alleen voor `action == "commit"`,
    op precies de twee plekken waar een verse map anders stilzwijgend werd
    opgelost: een bestaande voorouder binnen een repo (`target = ancestor`) en
    een verse map buiten elke repo (die anders `continue` deed). Beide leveren
    dan `NEW_DIR` als map, in plaats van de voorouder resp. helemaal niets.
    Voor `push` verandert er niets: een push met een LETTERLIJKE
    main-bestemming (`to_main == "yes"`) heeft die mapkennis toch al niet
    nodig, en een push zonder is hier nooit de reden dat iets doorgelaten
    wordt.
    """
    found: list[Invocation] = []
    for step in walk(command, cwd):
        words = step.words
        idx = step.idx
        if basename(words[idx]) != "git":
            continue

        target, target_raw, from_new_dir, idx = git_target(step)
        if idx >= len(words):
            continue

        action = words[idx]
        if action not in ("commit", "push"):
            continue

        literal = step.literal
        # Een push met een expliciete main-bestemming telt ook als hij vanaf een
        # feature-branch gebeurt: `git push origin HEAD:main`, `+main`, `--all`.
        if action == "push":
            to_main = push_destination(
                words[idx + 1 :], literal[idx + 1 :], default_branch
            )
        else:
            to_main = "no"
        target, keep = settle_target(
            target, from_new_dir, action, to_main, report_new_dirs
        )
        if not keep:
            continue
        shown = target if target is not None else UNRESOLVED
        found.append((action, shown, to_main, target_raw))
    return found


def main(argv: list[str] | None = None) -> int:
    """CLI: `git_invocations.py [--report-new-dirs] [--default-branch B] <cwd>`,
    commando op stdin. `B` is de beschermde branch (default `main`).

    Print per gevonden invocation één tab-gescheiden regel op stdout
    (`<actie>\\t<map-of-marker>\\t<naar-main>\\t<ruwe-tekst>`). Geeft 0, of 3
    bij een open quote (Unparseable) — dezelfde twee uitkomsten als de oude
    heredoc-aanroep in main-commit-guard.sh.
    """
    args = sys.argv[1:] if argv is None else argv
    report_new_dirs = False
    default_branch = "main"
    while args and args[0] in ("--report-new-dirs", "--default-branch"):
        if args[0] == "--report-new-dirs":
            report_new_dirs = True
            args = args[1:]
        else:
            default_branch = args[1]
            args = args[2:]
    cwd = args[0]
    command = sys.stdin.read()
    try:
        invocations = find_invocations(
            command, cwd, report_new_dirs=report_new_dirs, default_branch=default_branch
        )
    except Unparseable:
        return 3
    for action, shown, to_main, target_raw in invocations:
        print(f"{action}\t{shown}\t{to_main}\t{target_raw}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
