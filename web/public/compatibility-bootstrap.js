// Run synchronously before either entry point's theme script and before its first paint.
// Only this exact historical preference is copied; canonical presence (including empty) wins.
(function () {
  try {
    if (localStorage.getItem("persona.theme") === null) {
      var theme = localStorage.getItem("jarvis.theme");
      if (theme !== null) localStorage.setItem("persona.theme", theme);
    }
  } catch (_) {}
})();
