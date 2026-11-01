// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=de831df2";
import { initTheme } from "./map.js?v=de831df2";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
