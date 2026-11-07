// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=548ba90f";
import { initTheme } from "./map.js?v=548ba90f";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
