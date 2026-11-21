---
type: fix
---
Go-tests committen hun fixture-repo's niet meer in de echte repository als `GIT_DIR` in de omgeving staat (bijvoorbeeld bij `go test` vanuit een git-hook). `internal/gitenvscrub` haalt de git-variabelen weg die een commando naar een andere repository sturen; elk testpakket dat git start importeert het, en een bewakingstest meldt een pakket dat dat vergeet.
