// The install page: the theme button and the download.

import { initDownload } from "./download.js";
import { initTheme } from "./map.js";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
