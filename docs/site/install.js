// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=ce0ead53";
import { initTheme } from "./map.js?v=ce0ead53";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
