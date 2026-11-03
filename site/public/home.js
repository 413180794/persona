// Persona home: install tabs + copy, the served personad version, the live console, scroll reveals, card glow.
(() => {
  document.documentElement.classList.remove("no-js");
  const reduce = matchMedia("(prefers-reduced-motion: reduce)").matches;

  // Copy buttons: the command next to them (the visible tab in the install box), without the prompt and comments.
  const commandText = (pre) => pre.textContent.replace(/^\s*\$\s*/, "").replace(/\s+#.*$/, "").trim();
  const copyFrom = (button, pre) => button.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(commandText(pre()));
      button.classList.add("done");
      button.firstElementChild.textContent = "已复制";
      setTimeout(() => { button.classList.remove("done"); button.firstElementChild.textContent = "复制"; }, 1600);
    } catch { /* no clipboard: the command stays selectable */ }
  });

  const box = document.querySelector("[data-tabs]");
  if (box) {
    const tabs = box.querySelectorAll("[data-tab]");
    const panes = box.querySelectorAll("[data-pane]");
    tabs.forEach((t) => t.addEventListener("click", () => {
      tabs.forEach((x) => x.setAttribute("aria-selected", String(x === t)));
      panes.forEach((p) => { p.hidden = p.dataset.pane !== t.dataset.tab; });
    }));
    const copy = box.querySelector(".copy");
    if (copy) copyFrom(copy, () => [...panes].find((p) => !p.hidden));
  }
  document.querySelectorAll(".cmd").forEach((c) => {
    const copy = c.querySelector(".copy"), pre = c.querySelector("pre");
    if (copy && pre) copyFrom(copy, () => pre);
  });

  // The version the site serves right now (/dl/VERSION, written by publish.sh).
  const ver = document.querySelector("[data-version]");
  if (ver) {
    fetch("/dl/VERSION", { cache: "no-store" }).then((r) => (r.ok ? r.text() : "")).then((v) => {
      v = v.trim();
      if (!/^v?\d/.test(v)) return;
      const short = v.replace(/-g?[0-9a-f]{7,}(-dirty)?(?=-\d{12}$)/, "").replace(/-\d{12}$/, "");
      ver.querySelector(".ver-text").textContent = `personad ${short}`;
      ver.title = v;
      ver.hidden = false;
    }).catch(() => {});
  }

  // The hero console: made-up events, one every couple of seconds.
  const feed = document.querySelector("[data-feed]");
  const events = [
    ["t-ok", "工作中", "MacBook › weather · 跑测试"],
    ["t-hot", "需要你", "Mac Studio › photo-sync · 计划等你审批"],
    ["t-ai", "助手", "今天有 3 件事需要你"],
    ["t-ok", "完成", "server › /orders 变慢 · 找到慢查询"],
    ["t-hot", "需要你", "MacBook › tests · Claude 在问你"],
    ["t-ai", "AI 分身", "起草了 2 条回复，等你发送"],
    ["t-ok", "工作中", "Mac Studio › blog · 迁移到 Astro"],
    ["t-ai", "/agent", "发布群 · 已以你的身份回复"],
  ];
  if (feed && !reduce) {
    let i = 3;
    setInterval(() => {
      if (document.hidden) return;
      const [cls, tag, text] = events[i++ % events.length];
      const li = document.createElement("li");
      li.innerHTML = `<b class="${cls}"></b>`;
      li.firstChild.textContent = tag;
      li.append(" " + text);
      feed.append(li);
      while (feed.children.length > 4) feed.firstElementChild.remove();
    }, 2400);
  }

  // Reveal on scroll.
  const io = new IntersectionObserver((entries) => entries.forEach((e) => {
    if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); }
  }), { rootMargin: "0px 0px -8% 0px", threshold: 0.08 });
  document.querySelectorAll(".reveal").forEach((el) => io.observe(el));

  // The glow follows the pointer on the feature cards; the phone drifts a little with the scroll.
  document.querySelectorAll(".cell").forEach((c) => c.addEventListener("pointermove", (e) => {
    const r = c.getBoundingClientRect();
    c.style.setProperty("--mx", `${e.clientX - r.left}px`);
    c.style.setProperty("--my", `${e.clientY - r.top}px`);
  }));
  const device = document.querySelector(".device");
  if (device && !reduce) {
    addEventListener("scroll", () => device.style.setProperty("--py", `${Math.min(scrollY, 600) * -0.08}px`), { passive: true });
  }
})();
