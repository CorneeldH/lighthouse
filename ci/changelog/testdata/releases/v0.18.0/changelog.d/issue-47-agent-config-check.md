---
type: added
---
`lighthouse agent-audit` lists the hooks, permission rules, skills, agents, rules and MCP servers of a repository, its user and its enabled plugins, each with its source, and reports conflicts between them and project hooks that overlap a lighthouse hook; it changes nothing. The first `lighthouse init`, and `lighthouse init --upgrade --audit`, end with a short summary, and the new skill `/lighthouse-audit-agent-config` judges each overlap candidate. See `docs/agent-audit.md`.
