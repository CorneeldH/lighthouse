// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=f4f4f75c";
import { initTheme } from "./map.js?v=f4f4f75c";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
