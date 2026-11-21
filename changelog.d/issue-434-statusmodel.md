---
type: changed
---
Het statusmodel loopt door in tick en triage: elke Teruggegeven zet het issue op To do met één label, parkeren is To do met `blocked_issue` (de tick zet het terug), een `needs_*`-label op een Controller-issue start geen run, en triage zet `needs_shaping` bij poort 4c. `On hold` wordt nergens meer geschreven en is geen verplichte boardstatus meer. Na de upgrade gaan bestaande Controller-issues waarvan de laatste protocol-comment een eigen Teruggegeven is, één keer naar To do met een label.
