// Package agentaudit inventories the agentic configuration of a repository, its user and
// its plugins, and reports conflicts, overlap with lighthouse hooks and the always-loaded
// skill surface. It only reads; it never writes.
package agentaudit

// Source names, exactly as the output shows them.
const (
	SourceLighthouse = "lighthouse"
	SourceProject    = "project"
	SourceLocal      = "lokaal"
	SourceUser       = "gebruiker"
	// SourceManaged is the status line for managed settings, which the audit never
	// reads. Its COULD NOT CHECK is fixed and does not count in the exit code.
	SourceManaged = "managed"
	// SourcePluginPrefix starts the source name of a plugin: plugin:<name>.
	SourcePluginPrefix = "plugin:"
)

// States of a source.
const (
	StateOK         = "ok"
	StateAbsent     = "afwezig"
	StateSkipped    = "overgeslagen (--repo-only)"
	StateCouldNotCh = "COULD NOT CHECK"
)

// Status is the outcome of reading one source. Reason is set with StateCouldNotCh.
type Status struct{ Source, State, Reason string }

// Hook is one registered command hook.
type Hook struct {
	Source, Event, Matcher, Command string
	File                            string // the settings file that registers it, as shown
	Script                          string // resolved path, "" for an inline command
	Content                         []byte // the script, or the command line
	Blocking                        bool
	Unreadable                      string // why the script could not be read, "" when it could
}

// Skill is a skill, command or agent whose name and description are always loaded.
// Kind is "skill", "command" or "agent". Harness is "claude" for what Claude Code loads,
// "codex" for .agents/skills/: inventoried, but not part of the Claude Code surface.
type Skill struct {
	Source, Name, Kind, Path string
	Harness                  string
	DescChars, NameChars     int
}

// Rule is a file under a rules directory; HasPaths says its frontmatter scopes it.
type Rule struct {
	Source, Path string
	HasPaths     bool
}

// Permission is one permission rule; Kind is "allow", "deny" or "ask".
type Permission struct{ Source, Kind, Rule string }

// MCPServer is one configured MCP server; Origin is the file it is configured in.
type MCPServer struct{ Source, Name, Origin string }

// Inventory is everything Collect found, each item with its source.
type Inventory struct {
	Statuses    []Status
	Hooks       []Hook
	Skills      []Skill
	Rules       []Rule
	Permissions []Permission
	MCP         []MCPServer
	Scripts     []string // files under .claude/hooks/ and <ConfigDir>/hooks/, outside HookDir, as <source>/<path>
}
