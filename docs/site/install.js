// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=16b7e1c1";
import { initTheme } from "./map.js?v=16b7e1c1";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
