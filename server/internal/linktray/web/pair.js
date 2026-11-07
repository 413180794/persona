// The pairing window: active mode takes the link from the phone or the web, passive mode makes one for the hub's
// admin page. The tray checks links and does the pairing (linktray.ParseLink, Host.Pair); this draws and asks.
"use strict";

(() => {
  const { el, icon, line, infobar, send, labels: L } = lt;
  const $ = (id) => document.getElementById(id);
  let state = lt.boot.pair;
  let tab = lt.boot.tab || (state.mode === "passive" ? "passive" : "active");

  // What the user typed survives a redraw (the tray pushes new state when link.json changes).
  const form = { link: lt.boot.prefill || "", machine: "", advanced: false, preview: null, result: null, busy: false,
    listen: state.listen, invitation: null, passiveError: "", generating: false, newAddress: "", addressError: "",
    copied: "" };

  function tabs() {
    const make = (id, text) => el("button", {
      class: "tab", role: "tab", "aria-selected": String(tab === id), "aria-controls": id,
      onclick: () => { tab = id; draw(); },
    }, text);
    $("tabs").replaceChildren(make("active", L.mode_active), make("passive", L.mode_passive));
    $("active").hidden = tab !== "active";
    $("passive").hidden = tab !== "passive";
  }

  function switchBack(text, mode) {
    if (!text) return null;
    return infobar("info", "info", el("span", { text }),
      el("div", {}, el("button", { class: "button", onclick: () => setMode(mode) }, L.switch_button)));
  }

  async function setMode(mode) {
    try {
      await send("set_mode", { mode });
    } catch (err) {
      form.result = { ok: false, text: err.message };
      draw();
    }
  }

  let parseTimer = 0;
  function onLink(value) {
    form.link = value;
    form.result = null;
    clearTimeout(parseTimer);
    parseTimer = setTimeout(async () => {
      form.preview = value.trim() ? await send("parse_link", { raw: value }).catch(() => null) : null;
      drawActive();
    }, 120);
  }

  async function pair() {
    form.busy = true;
    form.result = null;
    drawActive();
    try {
      const r = await send("pair", { raw: form.link, machine: form.machine });
      form.result = { ok: true, text: r.text };
      form.link = "";
      form.preview = null;
    } catch (err) {
      form.result = { ok: false, text: err.message };
    }
    form.busy = false;
    drawActive();
  }

  function drawActive() {
    const p = form.preview;
    const nodes = [
      el("h1", { class: "subtitle", text: state.active_paired ? L.pair_title_again : L.pair_title_new }),
      switchBack(state.switch_to_active, "active"),
    ];
    if (p && p.notice) nodes.push(infobar("caution", "warning", el("span", { text: p.notice })));
    nodes.push(el("div", { class: "secondary", text: L.active_intro }));
    const input = el("input", {
      class: "textbox code", type: "text", placeholder: "persona://link?…", spellcheck: "false", autocomplete: "off",
      value: form.link, oninput: (e) => onLink(e.target.value),
      onkeydown: (e) => { if (e.key === "Enter" && canPair()) pair(); },
    });
    nodes.push(input);
    if (p && p.kind === "ok") nodes.push(el("div", { class: "card lines" }, p.lines.map(line)));
    if (!(p && p.kind === "ok" && p.machine)) {
      const details = el("details", { open: form.advanced, ontoggle: (e) => { form.advanced = e.target.open; } },
        el("summary", {}, icon("chevron"), el("span", { text: L.advanced })),
        el("input", { class: "textbox", type: "text", placeholder: L.machine_placeholder, spellcheck: "false",
          value: form.machine, oninput: (e) => { form.machine = e.target.value; } }));
      nodes.push(details);
    }
    nodes.push(el("div", { class: "end" }, el("button", {
      class: "button accent", disabled: !canPair(), onclick: pair,
    }, form.busy ? L.pairing : L.pair_button)));
    if (form.result) {
      nodes.push(form.result.ok
        ? infobar("positive", "check_filled", el("span", { class: "selectable", text: form.result.text }))
        : infobar("danger", "octagon", el("span", { class: "selectable", text: form.result.text })));
    }
    nodes.push(addresses());
    const focused = document.activeElement && document.activeElement.classList.contains("code");
    $("active").replaceChildren(...nodes.filter(Boolean));
    if (focused) {
      input.focus();
      input.setSelectionRange(input.value.length, input.value.length);
    }
  }

  function canPair() {
    return !form.busy && form.link.trim() !== "" && !(form.preview && (form.preview.kind === "other_app" || form.preview.kind === "incomplete"));
  }

  function addresses() {
    const rows = state.addresses.map((a) => el("div", { class: "address" },
      icon("network"),
      el("span", { class: "text selectable", text: a.text }),
      a.in_use ? el("span", { class: "in-use caption", text: L.address_in_use }) : null,
      a.manual ? el("button", { class: "button subtle", onclick: () => removeAddress(a.url) }, L.address_remove) : null));
    const add = el("div", { class: "field-row" },
      el("input", { class: "textbox", type: "text", placeholder: L.address_add_placeholder, spellcheck: "false",
        value: form.newAddress, disabled: !state.active_paired,
        oninput: (e) => { form.newAddress = e.target.value; },
        onkeydown: (e) => { if (e.key === "Enter") addAddress(); } }),
      el("button", { class: "button", disabled: !state.active_paired, onclick: addAddress }, L.address_add));
    return el("div", { class: "group" },
      el("div", { class: "strong", text: L.addresses_title }),
      el("div", { class: "caption secondary", text: L.addresses_hint }),
      state.system_trust ? el("div", { class: "line" }, icon("shield"), el("div", { class: "text", text: L.addresses_trust })) : null,
      rows.length ? rows : el("div", { class: "secondary", text: L.addresses_empty }),
      add,
      form.addressError ? infobar("danger", "octagon", el("span", { text: form.addressError })) : null);
  }

  async function addAddress() {
    const raw = form.newAddress.trim();
    if (!raw) return;
    try {
      await send("address_add", { raw });
      form.newAddress = "";
      form.addressError = "";
    } catch (err) {
      form.addressError = err.message;
    }
    drawActive();
  }

  async function removeAddress(url) {
    try {
      await send("address_remove", { url });
      form.addressError = "";
    } catch (err) {
      form.addressError = err.message;
    }
    drawActive();
  }

  async function generate() {
    form.generating = true;
    form.passiveError = "";
    drawPassive();
    try {
      form.invitation = await send("pair_listen", { listen: form.listen });
    } catch (err) {
      form.passiveError = err.message;
    }
    form.generating = false;
    drawPassive();
  }

  function copy(text, which) {
    // In the click itself, so the page may write the clipboard.
    const area = el("textarea", { class: "offscreen" });
    area.value = text;
    document.body.appendChild(area);
    area.select();
    document.execCommand("copy");
    area.remove();
    form.copied = which;
    drawPassive();
    setTimeout(() => { form.copied = ""; drawPassive(); }, 2000);
  }

  function copyButton(text, which) {
    const done = form.copied === which;
    return el("button", { class: "button", onclick: () => copy(text, which) }, icon(done ? "check" : "copy"), done ? L.copied : L.copy);
  }

  function drawPassive() {
    const inv = form.invitation;
    const nodes = [
      el("h1", { class: "subtitle", text: L.passive_title }),
      el("div", { class: "secondary", text: L.passive_intro }),
      state.passive_text ? infobar("info", "info", el("span", { text: state.passive_text })) : null,
      switchBack(state.switch_to_passive, "passive"),
      el("div", { class: "field-row" },
        el("span", { class: "secondary", text: L.listen_label }),
        el("input", { class: "textbox code", type: "text", value: form.listen, spellcheck: "false",
          oninput: (e) => { form.listen = e.target.value; } }),
        el("button", { class: "button accent", disabled: form.generating, onclick: generate },
          form.generating ? L.generating : L.generate)),
      form.passiveError ? infobar("danger", "octagon", el("span", { class: "selectable", text: form.passiveError })) : null,
    ];
    if (inv) {
      nodes.push(el("div", { class: "card" }, el("div", { class: "stack" },
        el("div", { text: L.code_intro }),
        el("div", { class: "field-row" },
          el("input", { class: "textbox code", type: "text", readonly: true, value: inv.link, onfocus: (e) => e.target.select() }),
          copyButton(inv.link, "link")),
        el("div", { class: "caption secondary selectable", text: inv.addr_text }))));
      nodes.push(el("div", { class: "group" },
        el("div", { class: "caption secondary", text: L.firewall_hint }),
        el("div", { class: "field-row" }, el("div", { class: "code-block selectable grow", text: inv.firewall }),
          copyButton(inv.firewall, "firewall")),
        el("div", { class: "caption secondary", text: L.firewall_proxy })));
    }
    $("passive").replaceChildren(...nodes.filter(Boolean));
  }

  function draw() {
    tabs();
    drawActive();
    drawPassive();
  }

  lt.on("pair", (m) => {
    state = m.pair;
    if (!form.listen) form.listen = state.listen;
    draw();
  });
  lt.on("prefill", (m) => {
    tab = "active";
    onLink(m.raw);
    draw();
  });
  lt.on("tab", (m) => { tab = m.tab; draw(); });

  form.preview = lt.boot.preview || null;
  draw();
})();
