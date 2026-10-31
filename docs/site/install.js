// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=d5f76863";
import { initTheme } from "./map.js?v=d5f76863";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
