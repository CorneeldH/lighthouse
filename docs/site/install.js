// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=64615d75";
import { initTheme } from "./map.js?v=64615d75";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
