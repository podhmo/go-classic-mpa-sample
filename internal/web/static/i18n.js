// Locale-dependent UI strings for the client layer. The server emits the
// current locale's dictionary as JSON in <html data-i18n>; scripts read it
// through t() here so no inline <script> is needed under CSP.

const dict = (() => {
  try {
    return JSON.parse(document.documentElement.dataset.i18n || "{}");
  } catch {
    return {};
  }
})();

// t looks up key and substitutes each "%s" with the next argument.
export function t(key, ...args) {
  let s = dict[key];
  if (s == null) return key;
  for (const a of args) s = s.replace("%s", String(a));
  return s;
}
