// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=a9efdd74";
import { initTheme } from "./map.js?v=a9efdd74";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
