// The entry page: the theme button and "Why lighthouse?" opened when a link
// points at it.

import { initTheme } from "./map.js";

initTheme(document.getElementById("theme-toggle"));

const why = document.getElementById("why");
if (why && location.hash === "#why") {
  why.open = true;
}
