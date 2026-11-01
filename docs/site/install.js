// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=ca84807a";
import { initTheme } from "./map.js?v=ca84807a";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
