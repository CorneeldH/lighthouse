---
type: fixed
---
Een hergebruikte forge-workspace volgt `origin/<branch>` ook na een rebase en force-push: commits die
patch-gelijk op origin staan, tellen niet meer als eigen werk. Heeft de workspace echt eigen werk of een
vuile werkboom, dan start de run niet stil op de oude `HEAD`. De dispatcher geeft de run terug met de
reden "werkplek heeft eigen werk dat niet op origin staat", de workspace en het aantal eigen commits.
