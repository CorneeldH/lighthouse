// The entry page: the theme button, the old "#download" link sent on to the install page, and
// "Who it is for" opened when a link points at it.

import { initTheme } from "./map.js?v=ce0ead53";

initTheme(document.getElementById("theme-toggle"));

if (location.hash === "#download") {
  location.replace("install.html");
}

const who = document.getElementById("who");
if (who && location.hash === "#who") {
  who.open = true;
}
