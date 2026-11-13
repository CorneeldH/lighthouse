// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=bffdfde0";
import { initTheme } from "./map.js?v=bffdfde0";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
