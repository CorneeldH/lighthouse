---
type: fixed
---
`run.sh` neemt het build-slot als bestand met noclobber (`O_EXCL`) in plaats van met `mkdir`. De uutils-`mkdir` van de CI-runner `ubuntu-26.04` is niet atomair: parallelle runs wonnen het slot soms tegelijk en bouwden dubbel, wat `TestRunShBuildsOnceAndNeverWaits` willekeurig rood maakte.
