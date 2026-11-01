// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=523763e3";
import { initTheme } from "./map.js?v=523763e3";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
