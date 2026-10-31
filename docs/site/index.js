// The entry page: the theme button, the download, and "Why lighthouse?" opened when a link
// points at it.

import { initDownload } from "./download.js";
import { initTheme } from "./map.js";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("download"));

const why = document.getElementById("why");
if (why && location.hash === "#why") {
  why.open = true;
}
