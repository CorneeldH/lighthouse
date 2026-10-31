// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=87b4b61c";
import { initTheme } from "./map.js?v=87b4b61c";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
