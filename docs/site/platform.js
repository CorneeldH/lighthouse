// The install tab a visitor sees first, and the release label; pure functions without the DOM.

const has = (text, words) => words.some((word) => text.includes(word));

export function choosePlatform(info) {
  const text = `${info.platform} ${info.userAgent}`.toLowerCase();
  const arch = info.architecture;
  // Android before Linux: Android user agents contain "Linux". No bare "win": "Darwin" has it.
  if (has(text, ["android", "iphone", "ipad"])) return { tab: null, note: null };
  if (has(text, ["windows", "win32", "win64"])) return { tab: "linux-amd64", note: "wsl" };
  if (text.includes("mac")) {
    // A browser rarely tells Apple silicon from Intel, so arm64 is the default.
    return arch === "x86" ? { tab: "darwin-amd64", note: null } : { tab: "darwin-arm64", note: "intel" };
  }
  if (text.includes("linux")) {
    const arm = arch === "arm" || has(text, ["aarch64", "arm64"]);
    return { tab: arm ? "linux-arm64" : "linux-amd64", note: null };
  }
  return { tab: null, note: null };
}

export function releaseLabel(data) {
  const tag = data?.tag_name;
  return typeof tag === "string" && /^v\d[\w.-]*$/.test(tag) ? tag : "latest release";
}
