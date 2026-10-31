// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=647b9a73";
import { initTheme } from "./map.js?v=647b9a73";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
