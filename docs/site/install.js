// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=d8f9721d";
import { initTheme } from "./map.js?v=d8f9721d";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
