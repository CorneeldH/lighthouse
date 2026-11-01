// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=0081d303";
import { initTheme } from "./map.js?v=0081d303";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
