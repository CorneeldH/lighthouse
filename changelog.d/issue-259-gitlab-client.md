---
type: added
---
Lighthouse spreekt nu met GitLab: `[forge] kind = "gitlab"` (met `host` voor een zelf gehoste GitLab en `GITLAB_TOKEN`) opent een GitLab-client achter dezelfde forge-interface, `serve` toont de runs van zo'n repo en `run` accepteert subgroepen. De status van een issue komt uit de ingebouwde issuestatus van GitLab (*On hold* en *Controller* moeten op de bovenste groep van de lifecycle staan), en `[forge.board]` kent voor GitLab alleen `status_options` en `priority_options`. De Python-scripts van `pack/core` blijven voor GitLab ongeschikt tot een follow-up.
