// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=6e674421";
import { initTheme } from "./map.js?v=6e674421";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
