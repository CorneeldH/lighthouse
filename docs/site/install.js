// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=524a6512";
import { initTheme } from "./map.js?v=524a6512";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
