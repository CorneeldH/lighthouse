---
type: changed
---
`IssueItem` en `Planning` van de GitHub-forge gebruiken geen GraphQL meer. Een leeg GraphQL-budget laat het opzoeken van een boarditem en het tabblad Prioritering daardoor niet meer falen. Een gearchiveerd item blijft vindbaar, ook in een nieuw proces, als een eerdere boardlezing het zag (tot 30 dagen daarna). `MilestoneCount.Done` telt nog altijd alleen als *completed* gesloten issues.
