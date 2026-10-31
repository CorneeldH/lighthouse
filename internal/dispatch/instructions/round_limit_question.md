- This repo's controller asks at the round limit; that goes above the rule "An adversarial review
  at `round_limit` is no question" above. Record the open points (`accept-open`, the block in
  the document) and commit, and then do not post the handoff but one question with
  `lighthouse ask`: the open points plus the go/no-go, with the options *continue*
  (recommended) and *hand back*. After *continue*: the usual handoff to the next phase. After
  *hand back*: no handoff and no question; stop without any further tool call. The controller
  then hands the issue back (no handoff or question).
