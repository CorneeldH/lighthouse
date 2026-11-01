// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=6a0b8fd1";
import { initTheme } from "./map.js?v=6a0b8fd1";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
