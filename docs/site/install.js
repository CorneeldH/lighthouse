// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=79c73d3e";
import { initTheme } from "./map.js?v=79c73d3e";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
