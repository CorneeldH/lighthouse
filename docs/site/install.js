// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=dd122809";
import { initTheme } from "./map.js?v=dd122809";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
