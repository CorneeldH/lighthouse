---
type: fixed
---
Een controller-run die de usage-gate parkeert schrijft zijn resume-marker nu naar `<LIGHTHOUSE_STATE_DIR>/resume`, de map die een kenn-forge-container deelt met de host, en de controller leest die map naast de eigen marker-map. Zo gaat een geparkeerde run in een container niet meer terug met *geen handoff of vraag*. Een marker die de controller niet kan lezen geeft de reden *geparkeerd, marker niet leesbaar*. (#513)
