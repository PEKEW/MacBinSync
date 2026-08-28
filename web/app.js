// macsync 前端：拉取 /api/report 并渲染仪表盘。
(function () {
  "use strict";

  const $ = (sel) => document.querySelector(sel);
  const content = $("#content");

  function esc(s) {
    return String(s ?? "").replace(/[&<>"']/g, (c) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
    }[c]));
  }

  function fmtSize(n) {
    if (!n) return "-";
    if (n < 1024) return n + " B";
    if (n < 1048576) return (n / 1024).toFixed(1) + " KB";
    if (n < 1073741824) return (n / 1048576).toFixed(1) + " MB";
    return (n / 1073741824).toFixed(1) + " GB";
  }

  function fmtTime(t) {
    if (!t) return "-";
    const d = new Date(t);
    return d.toLocaleString("zh-CN", { hour12: false });
  }

  let report = null;

  function section(title, badge, bodyHTML) {
    return `<section><h2><span>${esc(title)}</span><span class="badge">${esc(badge)}</span></h2>
      <div class="section-body" data-body>${bodyHTML}</div></section>`;
  }

  function kvSection(items) {
    return `<div class="kv">${items
      .map(([k, v]) => `<div class="item"><div class="k">${esc(k)}</div><div class="v">${esc(v)}</div></div>`)
      .join("")}</div>`;
  }

  function brewTable(items, kind) {
    if (!items.length) return `<div class="empty">未安装 ${kind === "cask" ? "cask" : "formula"}</div>`;
    return `<table><thead><tr><th>名称</th><th>版本</th><th>来源</th></tr></thead><tbody>${items
      .map((f) => {
        const tag = kind === "formula"
          ? (f.top_level ? '<span class="tag top">brew leaves</span>' : '<span class="tag dep">依赖</span>')
          : "";
        const od = f.outdated ? '<span class="tag outdated">可更新</span>' : "";
        return `<tr><td class="name">${esc(f.name)}${tag}${od}</td><td class="mono">${esc(f.version)}</td><td class="mono">${esc(kind)}</td></tr>`;
      })
      .join("")}</tbody></table>`;
  }

  function render() {
    const m = report.machine;
    $("#machine-line").textContent = `${m.hostname || "本机"} · ${m.os_name || ""} ${m.os_ver || ""} · ${m.chip || ""} · 盘点于 ${fmtTime(report.generated_at)}`;

    let html = "";

    // 系统信息
    html += section("系统信息", m.hostname || "-", kvSection([
      ["主机名", m.hostname || "-"],
      ["系统", `${m.os_name || ""} ${m.os_ver || ""}`.trim() || "-"],
      ["芯片", m.chip || "-"],
      ["Shell", m.shell || "-"],
      ["Home", m.home || "-"],
      ["Brew 前缀", m.brew_prefix || "-"],
    ]));

    // brew
    const brew = report.brew;
    html += section("Homebrew", `${brew.formulae.length} formula · ${brew.casks.length} cask`,
      brewTable(brew.formulae, "formula") +
      `<h3 style="margin:14px 0 6px;color:var(--muted);font-size:13px;">Casks</h3>` +
      brewTable(brew.casks, "cask") +
      (brew.taps.length
        ? `<h3 style="margin:14px 0 6px;color:var(--muted);font-size:13px;">Taps</h3><div class="grid">${brew.taps
            .map((t) => `<span class="chip mono">${esc(t)}</span>`).join("")}</div>`
        : ""));

    // uv
    const uv = report.uv;
    html += section("uv 工具", `${uv.tools.length} 个`, uv.tools.length
      ? `<div class="grid">${uv.tools
          .map((t) => `<span class="chip">${esc(t.name)} <small>v${esc(t.version)}</small></span>`)
          .join("")}</div>
         <div style="margin-top:10px;color:var(--muted);font-size:12px;">${uv.tools
           .map((t) => `${esc(t.name)}: ${t.binaries.map(esc).join(", ")}`)
           .join(" · ")}</div>`
      : `<div class="empty">未安装 uv 工具</div>`);

    // runtimes
    const rt = report.runtimes;
    let rtHtml = kvSection([
      ["Rustup", rt.rustup_toolchains.join("; ") || "-"],
      ["Node", rt.node?.version || "-"],
      ["nvm 版本", rt.node?.nvm_versions?.join(", ") || "-"],
      ["npm 全局", rt.node?.global_packages?.join(", ") || "-"],
    ]);
    html += section("运行时", "rustup / node", rtHtml);

    // cargo
    const cargo = report.cargo;
    html += section("Cargo 二进制", `${cargo.bins.length} 个`, cargo.bins.length
      ? `<div class="grid">${cargo.bins.map((b) => `<span class="chip">${esc(b)}</span>`).join("")}</div>`
      : `<div class="empty">~/.cargo/bin 为空</div>`);

    // local bin
    const lb = report.local_bin;
    html += section("~/.local/bin 散装工具", `${lb.bins.length} 个`, lb.bins.length
      ? `<div class="grid">${lb.bins.map((b) => `<span class="chip">${esc(b)}</span>`).join("")}</div>
         <div style="margin-top:8px;color:var(--muted);font-size:12px;">uv / uvx 为 uv 管理器自身；其余为独立安装的 CLI（claude、synto、kimi 等）</div>`
      : `<div class="empty">~/.local/bin 为空</div>`);

    // apps
    const apps = report.apps;
    html += section("GUI 应用", `${apps.length} 个`, apps.length
      ? `<table><thead><tr><th>应用</th><th>版本</th><th>路径</th></tr></thead><tbody>${apps
          .map((a) => `<tr><td class="name">${esc(a.name)}</td><td class="mono">${esc(a.version) || "-"}</td><td class="mono">${esc(a.path)}</td></tr>`)
          .join("")}</tbody></table>`
      : `<div class="empty">/Applications 为空</div>`);

    // configs
    const cfgs = report.configs;
    html += section("配置文件", `${cfgs.length} 项`, cfgs.length
      ? cfgs
          .map((c) => {
            const meta = [
              c.type === "git-repo" ? "git" : c.type,
              c.file_count ? `${c.file_count} 文件` : "",
              c.hash ? c.hash.slice(0, 8) : "",
              fmtSize(c.size),
              fmtTime(c.mod_time),
            ].filter(Boolean).join(" · ");
            return `<div class="cfg-row"><span class="p">${esc(c.path)}</span><span class="meta">${esc(meta)}</span></div>`;
          })
          .join("")
      : `<div class="empty">未发现配置文件</div>`);

    content.innerHTML = html;
  }

  // 全局过滤
  let filterTimer = null;
  $("#filter").addEventListener("input", (e) => {
    clearTimeout(filterTimer);
    filterTimer = setTimeout(() => {
      const q = e.target.value.trim().toLowerCase();
      document.querySelectorAll("section").forEach((sec) => {
        if (!q) { sec.classList.remove("hidden"); return; }
        const hay = sec.textContent.toLowerCase();
        sec.classList.toggle("hidden", !hay.includes(q));
      });
    }, 120);
  });

  // 刷新
  $("#refresh").addEventListener("click", async () => {
    const btn = $("#refresh");
    btn.disabled = true;
    btn.textContent = "盘点中…";
    content.innerHTML = `<div class="loading">正在重新盘点本机（brew / uv / 运行时 / 应用 / 配置）…</div>`;
    try {
      const res = await fetch("/api/report", { method: "POST" });
      report = await res.json();
      render();
    } catch (err) {
      content.innerHTML = `<div class="loading">盘点失败: ${esc(err.message)}</div>`;
    } finally {
      btn.disabled = false;
      btn.textContent = "↻ 重新盘点";
    }
  });

  // 初始加载
  fetch("/api/report")
    .then((r) => (r.ok ? r.json() : Promise.reject(new Error("HTTP " + r.status))))
    .then((data) => { report = data; render(); })
    .catch((err) => {
      content.innerHTML = `<div class="loading">加载失败: ${esc(err.message)}<br><br>
        <span style="color:var(--muted)">试试点右上角「重新盘点」</span></div>`;
    });
})();
