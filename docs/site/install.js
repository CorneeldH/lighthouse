// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=2c5ca597";
import { initTheme } from "./map.js?v=2c5ca597";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
