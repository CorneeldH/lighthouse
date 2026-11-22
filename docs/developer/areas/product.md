# Area: product

## Crux

Iemand die niet Corneel is, installeert lighthouse zonder hulp en landt een PR. De handmatige keten
werkt al zonder controller, maar alleen op de opzet waar hij gebouwd is. Een ander moet hem kunnen
downloaden, met een agent kunnen installeren en via de keten één echt issue tot een PR kunnen
brengen, zonder Go, zonder Corneels paden en zonder dat Corneel meekijkt.

## Leidend beleid

- Installeren gaat via een agent met één handleiding: de agent doet zelf wat kan en vraagt de rest,
  elke keuze met een aanbevolen antwoord
  ([installatiehandleiding voor agents](../planning/specs/2026-10-31-issue-88-agent-installatiehandleiding-design.md),
  [Product](https://github.com/CorneeldH/lighthouse/milestone/6)).
- Releases gaan automatisch, met binaries per platform en checksums; een release komt alleen van een
  commit waarop de macOS-tests groen waren
  ([releasebeleid](../planning/specs/2026-10-27-releasebeleid-design.md),
  [macOS-releasepoort](../planning/specs/2026-11-14-macos-releasepoort-design.md)).
- De download is eenvoudig: een statische site zonder buildstap en zonder script dat je laat draaien
  ([website-download](../planning/specs/2026-10-31-website-download-design.md)).
- Een andere opzet telt pas als iemand anders er een echt issue mee tot een PR brengt; wat die
  terugmeldt, wordt een issue hier
  ([M3](../planning/specs/2026-11-07-m3-tweede-omgeving-design.md),
  [Product](https://github.com/CorneeldH/lighthouse/milestone/6)).

## Bewust niet

- **N1** Een installer die Claude Code of plugins zonder akkoord installeert; de agent vraagt eerst
  ([Product](https://github.com/CorneeldH/lighthouse/milestone/6),
  [installatiehandleiding voor agents](../planning/specs/2026-10-31-issue-88-agent-installatiehandleiding-design.md)).
- **N2** Ondertekenen of notariseren, Homebrew en Scoop
  ([releasebeleid](../planning/specs/2026-10-27-releasebeleid-design.md)).
- **N3** Hooks op native Windows; de binary draait er, de hooks vragen WSL
  ([releasebeleid](../planning/specs/2026-10-27-releasebeleid-design.md),
  [M3](https://github.com/CorneeldH/lighthouse/milestone/4)).
- **N4** Controller, `serve` of SPA voor een nieuwe gebruiker → dispatcher; de eerste versie is de
  handmatige keten ([Product](https://github.com/CorneeldH/lighthouse/milestone/6),
  [website-download](../planning/specs/2026-10-31-website-download-design.md)).
- **N5** Docs voor wie aan lighthouse werkt → meta ([labels](../labels.md)).
