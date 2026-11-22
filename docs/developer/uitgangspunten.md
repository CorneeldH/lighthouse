# Uitgangspunten

Overtuigingen die een beslissing in lighthouse sneller maken, elk in de vorm "X boven Y". Elk heeft
een tegendeel dat iemand redelijkerwijs zou kiezen, en minstens twee beslissingen als bewijs. Citeer
er een als `uitgangspunt C`; de strategie staat in [strategie.md](strategie.md).

**Voorrang.** Een harde regel (`AGENTS.md`, een hook, een gate) wint altijd van een uitgangspunt. Bij
een conflict tussen uitgangspunten wint de hogere: de volgorde in dit bestand is de voorrang. De
letters liggen vast; een vervallen uitgangspunt houdt zijn letter, met `(vervallen)` in de kop.

A en B zeggen wat lighthouse is, C tot en met G hoe de keten dat waarmaakt. C staat niet in conflict
met A: zonder bewijs geldt A niet. F staat boven G: een overzichtslaag mag erbij, ook als dat een
onderdeel meer is.

## A. De mens bepaalt intent en taste, de keten doet de rest op bewijs, boven de mens als ritueel goedkeurder

Vertrouwen geldt per soort wijziging en is intrekbaar.

*Waarom*: een goedkeuring die de uitkomst niet verandert, kost aandacht en levert niets.

*Bewijs*: [#294](planning/specs/2026-11-08-issue-294-tick-merget-design.md), [#121](planning/specs/2026-11-16-issue-121-geplande-runs-design.md), [pins](planning/specs/2026-11-15-issue-325-pins-design.md)

## B. Het doel van een praktijk boven zijn menselijke vorm

*Waarom*: lighthouse is AI-native: een praktijk blijft om wat hij bereikt, niet omdat mensen hem zo
deden.

*Bewijs*: [repo zonder Python](planning/specs/2026-11-16-repo-zonder-python-design.md), [handoffs](planning/specs/2026-11-01-handoffs-write-pr-close-issue-design.md), [gate na commit](planning/specs/2026-10-27-gate-na-commit-design.md)

## C. Luid "kon niet controleren" boven stil groen

Ook in metingen: een eerlijk label boven een mooie metriek.

*Waarom*: een gate die stil stopt met kijken, ziet eruit als schone code.

*Bewijs*: [zonder Python](planning/specs/2026-11-07-zonder-python-design.md), [canary per regel](planning/specs/2026-11-19-canary-per-regel-design.md), [kaarten](planning/specs/2026-10-27-kaarten-design.md)

## D. Een check boven een oordeel of een herinnering

Wat te controleren is, wordt een check, en een fout wordt er een.

*Waarom*: een check geeft op elke machine hetzelfde oordeel; een herinnering niet.

*Bewijs*: [releasebeleid](planning/specs/2026-10-27-releasebeleid-design.md), [leesgrens triage](planning/specs/2026-11-01-triage-leesgrens-design.md), [beoordelingstabel](planning/specs/2026-11-21-issue-390-beoordelingstabel.md)

## E. Eén bron boven een kopie die kan afdrijven

*Waarom*: twee bronnen lopen stil uit elkaar.

*Bewijs*: [forge via REST](planning/specs/2026-11-17-forge-github-rest-design.md), [werkplek zonder kenn-forge](planning/specs/2026-11-07-werkplek-zonder-kenn-forge-design.md), [milestoneprioriteit](planning/specs/2026-11-20-milestone-prioriteit-design.md)

## F. Overzicht eerst, inzoomen waar nodig, boven alles tegelijk

*Waarom*: aandacht is schaars.

*Bewijs*: [systeemkaart](planning/specs/2026-10-31-systeemkaart-design.md), [plattegrond na init](planning/specs/2026-11-01-plattegrond-na-init-design.md), [prioritering](planning/specs/2026-11-16-prioritering-tabblad-design.md)

## G. Weinig onderdelen die bewezen iets toevoegen boven meer machinerie

Dit geldt voor *wat* we bouwen, niet voor hoe grondig een gate controleert.

*Waarom*: elk onderdeel kost context, tokens en uitleg.

*Bewijs*: [spec-adversary opgeheven](planning/specs/2026-10-31-issue-143-spec-adversary-opheffen.md), [repo zonder Python](planning/specs/2026-11-16-repo-zonder-python-design.md), [website-download](planning/specs/2026-10-31-website-download-design.md)
