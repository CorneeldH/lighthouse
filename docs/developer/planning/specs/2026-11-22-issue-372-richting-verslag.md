# Richting vastleggen voor mens en model: verslag van het onderzoek

Issue: #372. Hoort bij het ontwerp [Richting per repo](2026-11-22-issue-372-richting-per-repo-design.md).
Milestone: *Systeemoverzicht: kaarten en richting die mens en model lezen*.

## De vraag

Hoe leg je roadmap, strategie en uitgangspunten (tenets) van een repo zo vast dat mens en model er
allebei op sturen? Per bron: wat ze vastleggen, in welke vorm, hoe het in de praktijk gebruikt wordt,
en wat er misging.

Links naar personal_ai_agent (paia) wijzen naar commit `5cdaf572`, zodat de regelnummers kloppen.
personal_ai_agent is een privérepo: zonder toegang geven die links 404. Alle externe links zijn op
2026-11-22 gecontroleerd.

## Overzicht

| Bron | Wat ze vastleggen | Vorm | Wat misging |
|---|---|---|---|
| paia | roadmap, strategie, tenets, areas | losse md-bestanden, Rumelt-kern per area | alles met status veroudert; alleen wat een skill schrijft, blijft vers |
| Rick Pastoor | jaar- en weekreview, thema's per kwartaal | persoonlijk systeem; voor agents vrijwel niets openbaar | niet na te gaan |
| Amazon-tenets | overtuigingen die beslissen versnellen | "X boven Y", hoogstens 7, gerangschikt | open deuren, absoluten, een besluit verkleed als tenet |
| Shape Up | de pitch per cyclus | Problem, Appetite, Solution, Rabbit holes, No-gos | — (geen backlog, dus niets om te verouderen) |
| Now/Next/Later | horizonnen zonder datum | drie kolommen, problemen in Later | wordt "een vagere tijdlijn" zonder definitie per kolom |
| Rumelt | de kern van een strategie | diagnose, leidend beleid, samenhangende acties | "bafflegab", doelen als strategie |
| Bestanden voor agents | richting en conventies | Kiro steering, spec-kit constitution, BMAD, CLAUDE.md, AGENTS.md | te lang, te breed, niet gevolgd, onderhoud vaag |
| Impatient Programming | waarden, regels, het Contract | waarden, 29 regels, contractbestanden met checks | "A Contract nobody checks drifts from the code within a week" |
| Lighthouse zelf | beslissingen in 61 specs en 18 milestones | proza, verspreid | richting nergens samengevat; spanningen tussen specs |

## 1. personal_ai_agent (paia)

**Roadmap.** [`docs/ROADMAP.md`](https://github.com/CorneeldH/personal_ai_agent/blob/5cdaf5725f7ab60b6da7bd1ef498a000f2a090bb/docs/ROADMAP.md)
(138 regels) heeft een visiediagram (L3-16), lijsten NOW/NEXT/LATER/FUTURE met issues (L20-113) en
een tabel met gearchiveerde milestones (L124-134).

- Niets leest hem automatisch. `AGENTS.md` vraagt hem te lezen vóór issuewerk
  ([L212-214](https://github.com/CorneeldH/personal_ai_agent/blob/5cdaf5725f7ab60b6da7bd1ef498a000f2a090bb/AGENTS.md#L212-L214)).
- Hij is verouderd. De NOW-issues zijn uit juni (L55, L64, L75-76). #156 staat open (L119), maar
  is gesloten. Een "open beslissing" (L39-46) is elders al genomen.
- De structuur is van 2026-06-14; sindsdien kreeg hij alleen losse regels.

**Strategie.** [`docs/product/product-strategie.md`](https://github.com/CorneeldH/personal_ai_agent/blob/5cdaf5725f7ab60b6da7bd1ef498a000f2a090bb/docs/product/product-strategie.md)
(36 regels: Kern, Wat we bouwen, Doelgroep, Licentie) en een verdieping van 112 regels. Alleen
`tenets.md` en `areas/product.md` linken ernaar; geen skill leest ze.

**Tenets.** [`docs/developer/tenets.md`](https://github.com/CorneeldH/personal_ai_agent/blob/5cdaf5725f7ab60b6da7bd1ef498a000f2a090bb/docs/developer/tenets.md)
heeft twaalf tenets "X boven Y" met vaste letters A-M (L17-106), een voorrangsregel (een harde regel
wint altijd, L7-9) en een *Koppeling* met de spec-kop `Erft: tenets` (L108-120).

- Ze worden gebruikt. Van de 33 specs sinds 2026-10-11 dragen er 30 `Erft: tenets`, en 31 citeren
  `(tenet X)`. Letters B, H en J samen ruim honderd keer.
- De laadregel staat alleen in paia's `AGENTS.md`, niet in de instructies van een onbemande run.
  Het risico "tenets die niemand leest" uit de spec
  ([L303-305](https://github.com/CorneeldH/personal_ai_agent/blob/5cdaf5725f7ab60b6da7bd1ef498a000f2a090bb/docs/developer/planning/specs/2026-10-10-tenets-areablokken-vraagbundel-design.md#L303-L305))
  is dus maar half afgedekt.

**Areas.** [`docs/developer/areas/`](https://github.com/CorneeldH/personal_ai_agent/blob/5cdaf5725f7ab60b6da7bd1ef498a000f2a090bb/docs/developer/areas/index.md):
een index met de Rumelt-kern (L5) en een vuistregeltabel (L7-17), en negen bestanden van 17-18 regels.
Elk heeft `## Crux`, `## Leidend beleid` (de laatste bullet is altijd "Bewust **niet**", met de
buur-area) en `## Nabij doel`
([voorbeeld](https://github.com/CorneeldH/personal_ai_agent/blob/5cdaf5725f7ab60b6da7bd1ef498a000f2a090bb/docs/developer/areas/intake.md)).

- *Nabij doel* blijft vers: `lighthouse-close-issue` stap 5 schrijft het bij het sluiten van een
  milestone, ongeveer twaalf keer tussen 2026-10-11 en 2026-11-19.
- De crux wordt nooit bijgewerkt. Alle blokken staan nog op `Status: concept`; de geplande
  correctieronde per area kwam er niet.
- Crux en "Bewust niet" zijn proza zonder ID en worden zelden geciteerd: 4 van 33 specs noemen de
  crux, 3 "bewust niet".
- De vuistregel staat zowel in `areas/index.md` als in `labels.md` (L33-50): twee bronnen.

**Overige.** [`roadmap_thetaos_inspiratie.md`](https://github.com/CorneeldH/personal_ai_agent/blob/5cdaf5725f7ab60b6da7bd1ef498a000f2a090bb/docs/product/roadmap_thetaos_inspiratie.md)
(300 regels, opties A-H en fasen) is één keer geschreven en nooit bijgewerkt. `paths.history` heeft
geen lezer; de historiepagina's stoppen bij v4.01.

**De twee specs.** De [roadmap-spec](https://github.com/CorneeldH/personal_ai_agent/blob/5cdaf5725f7ab60b6da7bd1ef498a000f2a090bb/docs/developer/planning/specs/2026-07-26-roadmap-docs-living-site-design.md#L21-L35)
zegt: levende roadmapdocs horen niet in de issuetracker, en elk onderwerp heeft één canonieke plek.
De [tenets-spec](https://github.com/CorneeldH/personal_ai_agent/blob/5cdaf5725f7ab60b6da7bd1ef498a000f2a090bb/docs/developer/planning/specs/2026-10-10-tenets-areablokken-vraagbundel-design.md#L12-L63)
vond in 291 vraag-antwoordparen uit 67 specs dat twee derde van de aanbevelingen van de agent
ongewijzigd terugkwam, en dat bestaande docs maar 7% beantwoordden. Anti-doelen: tenets als wetboek,
een tweede ROADMAP, en meer lezen per sessie.

**Lessen.**

1. Scheid richting van status. Wat status draagt, veroudert binnen weken.
2. Een document blijft alleen vers als een skill het schrijft of leest.
3. Korte, stabiele ID's worden geciteerd; proza zonder ID niet.
4. "Bewust niet, hoort bij area X" is de werkende toets voor "willen we dit?".
5. Eén drager per feit: de vuistregel niet in twee bestanden.

## 2. Rick Pastoor

Openbaar is er vrijwel niets over hoe hij roadmap of strategie voor agents vastlegt.

- In [Denktank #162](https://www.denkproducties.nl/podcast/denktank-162-rick-pastoor-over-ai-hoe-een-persoon-straks-het-werk-van-een-heel-team-doet)
  vertelt hij dat hij Cirrux alleen bouwt met AI als "leger van digitale medewerkers". De pagina
  noemt "context is belangrijker dan één perfecte prompt" en "agents aansturen als medewerkers";
  een transcript ontbreekt.
- In [POM S11E30](https://www.pom.show/p/een-spamfilter-bouwen-is-heel-leuk) (samenvatting): agents
  hebben "veel meer context ... dan één zinnetje" nodig. Hij noemt het werk "verslavend" en
  concentratie die verdwijnt door contextwisselingen.
- In Cirrux krijgen agents afgebakende rechten, en een activiteitenlog markeert wat een AI deed
  ([LinkedIn](https://nl.linkedin.com/posts/rickpastoor_stomende-release-op-stomende-dag-cirrux-activity-7475536047066554368-qDG4)).
- Zijn methode uit *Grip*, van vóór AI: een [jaarreview](https://rickpastoor.com/sections/yearlyreview)
  met verantwoordelijkheidsgebieden en een thema per kwartaal, en een
  [weekreview](https://rickpastoor.com/sections/weeklyreview) die lopende projecten naar een
  "trusted system" brengt.

**Niet na te gaan:** welke contextdocumenten hij agents geeft, en wat er misging. De audio van de
podcasts kan dat bevatten.

**Les.** Gebieden met een periodieke review (jaar, kwartaal, week) is dezelfde vorm als areas met een
herziening; de review is het schrijfmoment.

## 3. Tenets (Amazon-stijl)

- Een tenet is "an organisational belief that accelerates decision-making": één idee met een korte
  reden, met een acceptabel tegendeel, hoogstens zeven, in aflopende voorrang, en de set eindigt met
  "unless you know better ones"
  ([AWS](https://www.amazonaws.cn/en/blog-selection/tenets-supercharging-decision-making/)).
- Valkuilen: open deuren, mantra's, en een besluit verkleed als tenet ("alleen Java").
- Charlie Kindel: tegenwoordige tijd, specifiek voor het team, gebruikt als tiebreaker "when making
  tough judgment calls". De meest gemaakte fout is een tenet die voor veel teams geldt en vrijwel niets
  zegt; absoluten zijn "a warning sign". Wijzig ze alleen "when you step back again"
  ([blog](https://blog.kindel.com/2020/02/10/tenets/)).

**Les.** Een tenet telt alleen als iemand redelijkerwijs het tegendeel zou kiezen. Volgorde is
betekenis: bij een conflict wint de hogere.

## 4. Shape Up

- Een pitch heeft vijf koppen: Problem, Appetite, Solution, Rabbit holes, No-gos. Pitches worden
  vooraf gelezen; aan de betting table wordt één cyclus vastgelegd
  ([hoofdstuk 6](https://basecamp.com/shapeup/1.5-chapter-06)).
- Er is geen backlog. Een pitch zonder inzet vervalt; "Really important ideas will come back"
  ([hoofdstuk 7](https://basecamp.com/shapeup/2.1-chapter-07)).

**Les.** De milestones van lighthouse zijn al pitches in deze vorm. Een rollende roadmap uit
milestones heeft geen apart document nodig.

## 5. Now/Next/Later

Drie horizonnen met afnemend detail, zonder datums (een echte deadline mag erbij); in Later ken je de
problemen, nog niet de oplossingen, en het advies is "prioritize problems, not ideas". "A horizon with
no shared definition just becomes a vaguer timeline"
([ProdPad](https://www.prodpad.com/blog/invented-now-next-later-roadmap/)).

**Les.** De rangorde van milestones (#468) is al een now/next/later. Een apart bestand zou een
tweede bron worden.

## 6. Rumelt

Een strategie heeft een kern van drie delen: diagnose ("What's going on here?"), leidend beleid en
samenhangende acties. Geen strategie: "bafflegab", doelen, en "do more better"
([samenvatting](https://sites.utexas.edu/curtispe/2011/08/19/what-is-strategy/),
[citaat](https://www.goodreads.com/quotes/11826614-the-kernel-of-a-strategy-contains-three-elements-1-a)).

**Les.** Diagnose en leidend beleid horen in een document. De samenhangende acties zijn de milestones.

## 7. Bestanden voor agents

- **Kiro steering**: `product.md`, `tech.md`, `structure.md` onder `.kiro/steering/`, standaard in
  elke interactie geladen, of op bestandspatroon, handmatig of automatisch. Advies: één domein per
  bestand en "explain why decisions were made". Eigen agents laden steering niet vanzelf
  ([docs](https://kiro.dev/docs/steering)).
- **spec-kit constitution**: kernprincipes, governance en een regel met versie en datum
  ([sjabloon](https://github.com/github/spec-kit/blob/main/templates/constitution-template.md)).
  `/speckit.plan` toetst een plan aan de artikelen als poort; de agent mag pas verder met een
  gedocumenteerde uitzondering
  ([spec-driven.md](https://github.com/github/spec-kit/blob/main/spec-driven.md)).
- **Birgitta Böckeler** over Kiro, spec-kit en Tessl: de agent volgde de instructies "frequently"
  niet allemaal, of paste ze te breed toe; de bestanden waren "very verbose and tedious to review"
  ([artikel](https://martinfowler.com/articles/exploring-gen-ai/sdd-3-tools.html)). Over context:
  "effectiveness goes down when it gets too much context"; laad lui wat niet altijd nodig is
  ([artikel](https://martinfowler.com/articles/exploring-gen-ai/context-engineering-coding-agents.html)).
- **BMAD**: projectcontext bevat alleen wat de code niet vertelt; een audit eindigt "smaller or
  equal, never larger"
  ([docs](https://docs.bmad-method.org/existing-codebases/set-and-maintain-project-context/)).
  Splits documenten boven ongeveer 50 KB, want agents "miss things in a long document"
  ([docs](https://docs.bmad-method.org/plan/prepare-long-documents-for-agents/)).
- **Claude Code**: CLAUDE.md is "context, not enforced configuration"; mik op minder dan 200 regels;
  tegenstrijdige instructies worden "arbitrarily" gekozen ([docs](https://code.claude.com/docs/en/memory)).
- **AGENTS.md** beveelt alleen operationele secties aan (build, test, stijl, PR), niets over richting
  ([agents.md](https://agents.md/)).
- **Onderzoek (ETH)**: contextbestanden verhogen het slagingspercentage "do not generally", en kosten
  meer dan 20% extra inferentie. Agents volgen de instructies wel, maar overzichten van de repo helpen
  niet ([arXiv 2602.11988](https://arxiv.org/abs/2602.11988)).

**Les.** Richting hoort niet in altijd-geladen context. Een agent leest hem op het moment van een
beslissing, als expliciete toets, en een mens keurt het oordeel.

## 8. Impatient Programming

[impatientprogramming.org](https://impatientprogramming.org/) (Obie Fernandez, CTO van ZAR) herziet XP
voor een team van mensen en agents. Gelezen: alle 34 openbare pagina's en de repo's en docs van de vijf
tools; niet gelezen: de pagina's voor leden.

**Methode.**

- XP's vijf waarden blijven "untouched"; Larry Walls luiheid, ongeduld en hoogmoed komen erbij, en
  "heart" ([waarden](https://impatientprogramming.org/values/)). Luiheid: "no coding agent ever makes
  the same mistake more than once".
- Het Contract staat bij de code: "If you can't rebuild a component from its Contract, the Contract is
  missing something", en "A guardrail is software, and building one is programming"
  ([XP revisited](https://impatientprogramming.org/xp-revisited/)).
- [Regels](https://impatientprogramming.org/rules/): 7 "Set a sustainable attention budget"; 9 let op
  de limieten van je abonnement, "because hitting one stops your agents mid-work"; 20 een agent die het
  Contract schrijft terwijl hij de code schrijft, kan "describing whatever it built"; 22 is ingetrokken,
  niet "out of nostalgia"; 26-29: alleen tests met een unieke vangst blijven, groen merget zichzelf,
  een bug betekent dat het Contract iets miste.
- [Tools](https://impatientprogramming.org/tools/): elke guardrail is "Deterministic" of "Subjective";
  "A Contract nobody checks drifts from the code within a week"; zelf mergen "only works when green
  means something".

**Essays en cases.**

- [Calabrese](https://impatientprogramming.org/essays/agusti-n-calabrese-one-person-a-coordinator-and-a-flock-running-impatient-progra/):
  wachten op zijn eigen beurt bepaalde de doorlooptijd; met een "written, expiring authorization"
  mergen PR's zonder hem, want zijn goedkeuringen veranderden "almost never" de uitkomst.
- [Salkini](https://impatientprogramming.org/essays/muhannad-salkini-rule-17-what-the-contract-can-t-hold/):
  houd verworpen aanpakken bij (wat, waarom, scope); "An empty caller list should mean 'unknown', not
  'safe'"; de agent leest de items van de code die hij gaat herbouwen voordat hij schrijft.
- [Scholl](https://impatientprogramming.org/essays/sebastian-scholl-stop-apologizing-for-being-the-bottleneck/):
  review is een dam; "You can't hold a model responsible for that call".
- [Margheim](https://impatientprogramming.org/essays/stephen-margheim-code-is-cheap-now/): "The codebase
  weighs more heavily on the agent's decisions than your prompt does."
- [Ryall](https://impatientprogramming.org/essays/mark-ryall-keep-pairing-with-humans/): pairen met
  mensen blijft, om wat het doel ervan was.
- [Born at ZAR](https://impatientprogramming.org/case-studies/born-at-zar/): componenten opnieuw
  gebouwd uit intent, beslisrecords en een uitvoerbaar Contract; "Include the derivation behind each
  decision, not only its value."
- [exhale](https://impatientprogramming.org/case-studies/exhale/): vijf stuurberichten van Obie, "None…
  contained code"; een tikfout in een commandonaam "had exited as a pass".

**Tools.**

- [covenant](https://github.com/tools4imps/covenant): `contracts/system/intent.md` en per component
  intent, verplichtingen en beslisrecords. Een record heeft `alternatives`, `evidence`, `approved_by`
  en `conditions` (premissen); `mix covenant.conditions` controleert de premissen die machinaal te
  controleren zijn, en een premisse zonder check heet "(not machine-checkable)". "Nothing is silently
  fine." ID's worden nooit hergebruikt; records worden vervangen, niet gewist.
- [flinch](https://github.com/tools4imps/flinch) en [qualm](https://github.com/tools4imps/qualm): een
  contract per onderdeel met genummerde verplichtingen. qualm controleert dat met een Go-test van
  80 regels in twee richtingen: verplichtingen zonder test, en tests die een verplichting noemen die
  niet bestaat. Exitcode 2 betekent "couldn't decide".
- [exhale](https://github.com/tools4imps/exhale-ruby): een uitzondering die code noemt die niet meer
  bestaat, laat de build falen.
- [praxis](https://zarpay.github.io/praxis-cli/): specs zijn "the developer's existing context files —
  READMEs, CLAUDE.md, AGENTS.md, whatever carries direction"; een reviewer meet of de code de richting
  volgt. "If you can write the check, write the check." "A spec that lags the code is a bug."
  `eval ci` faalt op een doel dat niet gecontroleerd is: "a gate that could not look is not a gate".

**Les.** Een richtingsdocument zonder check drijft af. Structuur controleert een test; een oordeel
("raakt dit issue een Bewust niet?") hoort bij een model of een mens. Een premisse zonder check wordt
geteld, niet stil goedgekeurd.

## 9. Lighthouse zelf: wat de beslissingen zeggen

Bron: de 61 specs in deze map en de 18 milestonebeschrijvingen. Een beslissing van Corneel weegt
zwaarder dan een "eigen keuze" van het model. Een patroon telt alleen als het in minstens twee
specs of milestones iets besliste.

| Patroon | Voorbeelden |
|---|---|
| Luid "kon niet controleren" boven stil groen | [zonder Python](2026-11-07-zonder-python-design.md), [canary per regel](2026-11-19-canary-per-regel-design.md), [pins](2026-11-15-issue-325-pins-design.md), [kaarten](2026-10-27-kaarten-design.md) ("een kaart die liegt is erger dan geen kaart") |
| Eén bron boven een kopie | [forge via REST](2026-11-17-forge-github-rest-design.md) (geen GraphQL-fallback), [werkplek zonder kenn-forge](2026-11-07-werkplek-zonder-kenn-forge-design.md) (één contract-suite), [milestoneprioriteit](2026-11-20-milestone-prioriteit-design.md) |
| Weinig onderdelen boven machinerie | [spec-adversary opgeheven](2026-10-31-issue-143-spec-adversary-opheffen.md), [repo zonder Python](2026-11-16-repo-zonder-python-design.md), [beoordelingstabel](2026-11-21-issue-390-beoordelingstabel.md) |
| Deterministisch boven modeloordeel | [releasebeleid](2026-10-27-releasebeleid-design.md), [leesgrens triage](2026-11-01-triage-leesgrens-design.md), [tick merget](2026-11-08-issue-294-tick-merget-design.md) |
| Autonomie op bewijs; de mens houdt instructies en config | [tick merget](2026-11-08-issue-294-tick-merget-design.md), [geplande runs](2026-11-16-issue-121-geplande-runs-design.md) tegenover [pins](2026-11-15-issue-325-pins-design.md) |
| Overzicht eerst | [systeemkaart](2026-10-31-systeemkaart-design.md), [plattegrond na init](2026-11-01-plattegrond-na-init-design.md), [prioritering](2026-11-16-prioritering-tabblad-design.md) |

**Platitudes**: uitgesproken, maar beslisten vrijwel niets: de SDP-toets uit de scope-spec,
harness-neutraliteit (alleen de keus voor kopiëren boven een plugin), "zelflerend" als kern, en de
grens van de fabriek.

**Spanningen** die het ontwerp niet oplost, maar wel zichtbaar maakt:

- "Lighthouse merget nooit" staat nog in de No-Gos van M4, *Werkplek zonder kenn-forge* en
  *GitLab-forge*, terwijl [#294](2026-11-08-issue-294-tick-merget-design.md) het omdraaide.
- "De UI blijft alleen-lezen" ([statusmodel](2026-11-20-statusmodel-en-statusschema-design.md)) tegenover
  de UI die de rang van een milestone naar de forge schrijft
  ([milestoneprioriteit](2026-11-20-milestone-prioriteit-design.md)).
- "Eerst eigen gereedschap" (scope-spec) tegenover "een vaardigheid voor elke repo".

**Les.** De richting van lighthouse bestaat al, verspreid over specs. Een document dat hem samenvat
en citeerbaar maakt, maakt ook de spanningen zichtbaar.

## 10. Wat over de bronnen heen geldt

1. **Richting zonder status.** Status staat op de forge (milestones, rangorde, board); richting in
   documenten (paia, Shape Up, Now/Next/Later).
2. **Elk document heeft een lezer en een schrijver.** Zonder schrijfmoment veroudert het (paia,
   Pastoors reviews, covenant).
3. **Kort, gerangschikt en citeerbaar.** Hoogstens zeven tenets met een echt tegendeel, en vaste ID's
   (AWS, Kindel, paia).
4. **Lezen bij een beslissing, niet altijd.** Altijd-geladen richting kost tokens zonder winst (ETH,
   Böckeler, BMAD, Kiro).
5. **Een toets als poort, met een mens die het oordeel keurt** (spec-kit, Scholl, praxis).
6. **Structuur wordt een check; een premisse zonder check wordt geteld** (covenant, qualm, praxis,
   exhale).
7. **"Bewust niet" is de toets voor "willen we dit?"** (paia-areas, Shape Up No-gos, Rumelt).
