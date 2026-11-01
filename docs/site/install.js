// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=d3a74df1";
import { initTheme } from "./map.js?v=d3a74df1";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
