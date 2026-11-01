// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=cb5549a8";
import { initTheme } from "./map.js?v=cb5549a8";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
