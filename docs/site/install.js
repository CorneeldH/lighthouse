// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=fa0ef5d6";
import { initTheme } from "./map.js?v=fa0ef5d6";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
