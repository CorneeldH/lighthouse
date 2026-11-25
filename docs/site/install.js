// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=c5e51222";
import { initTheme } from "./map.js?v=c5e51222";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
