// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=4f898adc";
import { initTheme } from "./map.js?v=4f898adc";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
