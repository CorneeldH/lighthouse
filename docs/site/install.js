// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=a148a307";
import { initTheme } from "./map.js?v=a148a307";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
