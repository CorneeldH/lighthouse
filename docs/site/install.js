// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=081086c5";
import { initTheme } from "./map.js?v=081086c5";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
