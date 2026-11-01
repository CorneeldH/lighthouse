// The install page: the theme button and the download.

import { initDownload } from "./download.js?v=886d61cc";
import { initTheme } from "./map.js?v=886d61cc";

initTheme(document.getElementById("theme-toggle"));
initDownload(document.getElementById("install"));
