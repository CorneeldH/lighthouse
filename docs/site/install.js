// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=668fdb53";
import { initTheme } from "./map.js?v=668fdb53";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
