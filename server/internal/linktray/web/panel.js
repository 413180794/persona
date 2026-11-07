// The panel: what the link is doing and what can be done about it. All text comes from the tray (linktray.View),
// so this only draws it.
"use strict";

(() => {
  const { el, icon, line, infobar, send, tell } = lt;
  const $ = (id) => document.getElementById(id);
  let noticeTimer = 0;
  let busy = "";

  $("mark").appendChild(icon("hotspot", "large"));

  function render(view) {
    const pill = $("pill");
    pill.className = "pill" + (view.tone ? " tone-" + view.tone : "");
    pill.textContent = view.pill;

    $("lines").replaceChildren(...view.lines.map(line));

    const rows = [];
    const tools = [];
    for (const a of view.actions) {
      if (a.id === "open_logs" || a.id === "quit") {
        tools.push(el("button", {
          class: "button subtle icon-only", title: a.hint ? a.label + " · " + a.hint : a.label, "aria-label": a.label,
          onclick: () => run(a),
        }, icon(a.icon)));
        continue;
      }
      if (a.checked !== undefined && a.checked !== null) {
        rows.push(el("button", {
          class: "row", role: "menuitemcheckbox", "aria-checked": String(a.checked), onclick: () => run(a),
        }, icon(a.icon), el("span", { class: "label", text: a.label }), el("span", { class: "switch" })));
        continue;
      }
      rows.push(el("button", {
        class: "row", role: "menuitem", disabled: a.disabled || busy === a.id, onclick: () => run(a),
      }, icon(a.icon), el("span", { class: "label", text: a.label })));
    }
    $("actions").replaceChildren(...rows);
    $("tools").replaceChildren(...tools);
  }

  // run sends an action; the tray answers with a new view, and maybe a notice.
  function run(a) {
    if (a.id === "autostart") {
      tell("action", { id: a.id, on: !a.checked });
      return;
    }
    busy = a.id;
    send("action", { id: a.id }).catch((err) => notice({ tone: "danger", text: err.message }))
      .finally(() => { busy = ""; });
  }

  function notice(n) {
    clearTimeout(noticeTimer);
    if (!n || !n.text) {
      $("notice").replaceChildren();
      return;
    }
    const icons = { positive: "check", danger: "octagon", caution: "warning", info: "info" };
    $("notice").replaceChildren(infobar(n.tone, icons[n.tone] || "info", el("span", { class: "selectable", text: n.text })));
    if (n.tone === "positive") noticeTimer = setTimeout(() => notice(null), 6000);
  }

  lt.on("view", (m) => render(m.view));
  lt.on("notice", (m) => notice(m.notice));
  lt.on("show", () => {
    // Replay the rise each time the panel opens.
    const main = $("main");
    main.classList.remove("rising");
    void main.offsetWidth;
    main.classList.add("rising");
  });

  $("version").textContent = "Persona Link " + lt.boot.version;
  render(lt.boot.view);
  lt.watchSize($("panel"));
})();
