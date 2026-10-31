// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=3f3f67c5";
import { initTheme } from "./map.js?v=3f3f67c5";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
