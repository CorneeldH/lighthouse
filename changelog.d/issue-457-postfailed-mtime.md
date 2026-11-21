---
type: fixed
---
An unreadable `post-failed.json` left by an earlier run in the same session no longer hands a later run back with *post mislukt: notitie onleesbaar*. Such a note is now dated by its file's modification time, which serve sends in the new `X-Lighthouse-Mod-Time` header on a node.
