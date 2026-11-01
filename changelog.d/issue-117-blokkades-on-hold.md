---
type: added
---
`lighthouse tick` parkeert een uitvoerend issue met een actieve voorganger: een `🤖 Geparkeerd`-comment met `blocked_by`, daarna het board naar On hold. Een half gelukte parkering maakt de volgende tick af, en zodra geen voorganger meer actief is zet tick het issue terug naar Controller. Een On hold dat een mens zette blijft staan. Het overzicht (`lighthouse serve`, `/api/questions`, `/api/returned`) toont alleen issues in Controller als de repo in `host.toml [dispatch]` staat met een `[forge.board]`.
