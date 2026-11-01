// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=ffbe4c8c";
import { initTheme } from "./map.js?v=ffbe4c8c";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
