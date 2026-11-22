// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=745c7a1f";
import { initTheme } from "./map.js?v=745c7a1f";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
