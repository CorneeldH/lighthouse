// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=97605b52";
import { initTheme } from "./map.js?v=97605b52";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
