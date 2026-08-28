// macsync 前端：仪表盘 + 工具交互（卸载 / 同步队列）。
(function () {
  "use strict";

  const $ = (sel) => document.querySelector(sel);
  const content = $("#content");

  function esc(s) {
    return String(s ?? "").replace(/[&<>"']/g, (c) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
    }[c]));
  }

  // 生成 data-sel 属性值（JSON），属性用单引号包裹，转义其中的单引号
  function selAttr(source, name, label) {
    return `data-sel='${JSON.stringify({ source, name, label }).replace(/'/g, "&#39;")}'`;
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
    return new Date(t).toLocaleString("zh-CN", { hour12: false });
  }

  let report = null;
  let queue = { items: [] };
  let currentItem = null; // 弹窗当前操作对象 {source, name, label}

  const UNINSTALLABLE = ["brew-formula", "brew-cask", "uv", "npm", "app"];
  const HINT = "点击任意工具可「卸载」或「加入同步队列」；队列将用于跨机器同步（M3）";

  function section(title, badge, bodyHTML) {
    return `<section><h2><span>${esc(title)}</span><span class="badge">${esc(badge)}</span></h2>
      <div class="section-body" data-body>${bodyHTML}</div></section>`;
  }

  function kvSection(items) {
    return `<div class="kv">${items
      .map(([k, v]) => `<div class="item"><div class="k">${esc(k)}</div><div class="v">${esc(v)}</div></div>`)
      .join("")}</div>`;
  }

  function chipGrid(items, source, labelOf) {
    if (!items.length) return `<div class="empty">无</div>`;
    return `<div class="grid">${items
      .map((it) => {
        const [name, label] = Array.isArray(it) ? it : [it, it];
        return `<span class="chip clickable" ${selAttr(source, name, labelOf ? labelOf(name, label) : label)} title="点击操作">${esc(label)}</span>`;
      })
      .join("")}</div>`;
  }

  function brewTable(items, kind) {
    if (!items.length) return `<div class="empty">无</div>`;
    return `<table><thead><tr><th>名称</th><th>版本</th><th>来源</th></tr></thead><tbody>${items
      .map((f) => {
        const od = f.outdated ? '<span class="tag outdated">可更新</span>' : "";
        const source = kind === "cask" ? "brew-cask" : "brew-formula";
        return `<tr class="clickable" ${selAttr(source, f.name, `${f.name} (brew ${kind})`)} title="点击操作">
          <td class="name">${esc(f.name)}${od}</td><td class="mono">${esc(f.version)}</td><td class="mono">${esc(kind)}</td></tr>`;
      })
      .join("")}</tbody></table>`;
  }

  function render() {
    const m = report.machine;
    $("#machine-line").textContent = `${m.hostname || "本机"} · ${m.os_name || ""} ${m.os_ver || ""} · ${m.chip || ""} · 盘点于 ${fmtTime(report.generated_at)}`;

    let html = `<div class="hint" style="margin-bottom:14px">${HINT}</div>`;

    // 系统信息
    html += section("系统信息", m.hostname || "-", kvSection([
      ["主机名", m.hostname || "-"],
      ["系统", `${m.os_name || ""} ${m.os_ver || ""}`.trim() || "-"],
      ["芯片", m.chip || "-"],
      ["Shell", m.shell || "-"],
      ["Home", m.home || "-"],
      ["Brew 前缀", m.brew_prefix || "-"],
    ]));

    // brew：leaves 主展示，依赖折叠
    const brew = report.brew;
    const leaves = brew.formulae.filter((f) => f.top_level);
    const deps = brew.formulae.filter((f) => !f.top_level);
    const depsHTML = deps.length
      ? `<details class="deps"><summary>依赖公式（${deps.length}，随 leaves 自动装入）</summary>${brewTable(deps, "formula")}</details>`
      : "";
    html += section("Homebrew", `${leaves.length} 主动安装 · ${deps.length} 依赖 · ${brew.casks.length} cask`,
      `<div class="hint">「主动安装」= brew leaves，即你显式安装的公式；「依赖」是它们自动拉入的，默认折叠。点击任意行可操作。</div>` +
      brewTable(leaves, "formula") +
      depsHTML +
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
          .map((t) => `<span class="chip clickable" ${selAttr("uv", t.name, `${t.name} (uv 工具)`)} title="点击操作">${esc(t.name)} <small>v${esc(t.version)}</small></span>`)
          .join("")}</div>
         <div style="margin-top:10px;color:var(--muted);font-size:12px;">${uv.tools
           .map((t) => `${esc(t.name)}: ${t.binaries.map(esc).join(", ")}`)
           .join(" · ")}</div>`
      : `<div class="empty">未安装 uv 工具</div>`);

    // runtimes
    const rt = report.runtimes;
    const npmChips = rt.node?.global_packages?.length
      ? `<div style="margin-top:8px"><div class="hint" style="margin-bottom:6px">npm 全局包（点击可操作）</div>
         <div class="grid">${rt.node.global_packages.map((g) => {
           const at = g.lastIndexOf("@");
           const pkg = at > 0 ? g.slice(0, at) : g;
           return `<span class="chip clickable" ${selAttr("npm", pkg, `${g} (npm 全局)`)} title="点击操作">${esc(g)}</span>`;
         }).join("")}</div></div>`
      : "";
    html += section("运行时", "rustup / node", kvSection([
      ["Rustup", rt.rustup_toolchains.join("; ") || "-"],
      ["Node", rt.node?.version || "-"],
      ["nvm 版本", rt.node?.nvm_versions?.join(", ") || "-"],
      ["npm 全局", rt.node?.global_packages?.join(", ") || "-"],
    ]) + npmChips);

    // cargo
    const cargo = report.cargo;
    html += section("Cargo 二进制", `${cargo.bins.length} 个`, cargo.bins.length
      ? chipGrid(cargo.bins.map((b) => [b, b]), "cargo", (n) => `${n} (cargo)`) +
        `<div class="hint" style="margin-top:8px">由 rustup 管理，不支持直接卸载；可加入同步队列。</div>`
      : `<div class="empty">~/.cargo/bin 为空</div>`);

    // local bin
    const lb = report.local_bin;
    html += section("~/.local/bin 散装工具", `${lb.bins.length} 个`, lb.bins.length
      ? chipGrid(lb.bins.map((b) => [b, b]), "local-bin", (n) => `${n} (~/.local/bin)`) +
        `<div class="hint" style="margin-top:8px">多为 uv / 独立安装器管理，删除风险高，仅支持加入同步队列。</div>`
      : `<div class="empty">~/.local/bin 为空</div>`);

    // apps
    const apps = report.apps;
    html += section("GUI 应用", `${apps.length} 个`, apps.length
      ? `<table><thead><tr><th>应用</th><th>版本</th><th>路径</th></tr></thead><tbody>${apps
          .map((a) => `<tr class="clickable" ${selAttr("app", a.name, `${a.name} (GUI 应用)`)} title="点击操作">
              <td class="name">${esc(a.name)}</td><td class="mono">${esc(a.version) || "-"}</td><td class="mono">${esc(a.path)}</td></tr>`)
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
            return `<div class="cfg-row clickable" ${selAttr("config", c.path, `${c.path} (配置)`)} title="点击操作">
              <span class="p">${esc(c.path)}</span><span class="meta">${esc(meta)}</span></div>`;
          })
          .join("")
      : `<div class="empty">未发现配置文件</div>`);

    content.innerHTML = html;
    bindRowClicks();
  }

  function bindRowClicks() {
    document.querySelectorAll("[data-sel]").forEach((el) => {
      el.addEventListener("click", () => {
        const { source, name, label } = JSON.parse(el.dataset.sel);
        openItemModal({ source, name, label: label || name });
      });
    });
  }

  // ---------- 弹窗 ----------

  function openItemModal(item) {
    currentItem = item;
    const inQ = queue.items.some((q) => q.source === item.source && q.name === item.name);
    $("#modal-title").textContent = item.label || item.name;
    $("#modal-uninstall").classList.toggle("hidden", !UNINSTALLABLE.includes(item.source));
    $("#modal-queue").classList.remove("hidden");
    $("#modal-cancel").textContent = "取消";
    $("#modal-queue").textContent = inQ ? "移出同步队列" : "进入同步队列";
    $("#modal-body").innerHTML = `
      <div class="item-detail">
        <div class="row"><span class="k">来源</span><span class="v mono">${esc(item.source)}</span></div>
        <div class="row"><span class="k">名称</span><span class="v mono">${esc(item.name)}</span></div>
        ${inQ ? '<div class="queued-tag">✓ 已在同步队列中</div>' : ""}
      </div>`;
    $("#modal").classList.remove("hidden");
  }

  function openQueueModal() {
    currentItem = null;
    $("#modal-title").textContent = `同步队列（${queue.items.length} 项）`;
    $("#modal-uninstall").classList.add("hidden");
    $("#modal-queue").classList.add("hidden");
    $("#modal-cancel").textContent = "关闭";
    $("#modal-body").innerHTML = queue.items.length
      ? queue.items
          .map((it, i) => `<div class="queue-row">
              <span class="v mono">${esc(it.name)}</span>
              <span class="meta">${esc(it.source)} · ${fmtTime(it.added_at)}</span>
              <button class="small danger-ghost" data-rm="${i}">移除</button>
            </div>`)
          .join("")
      : `<div class="empty">队列为空 — 在任意工具/配置上点击，选择「进入同步队列」</div>`;
    $("#modal").classList.remove("hidden");
    document.querySelectorAll("[data-rm]").forEach((b) =>
      b.addEventListener("click", async () => {
        const it = queue.items[+b.dataset.rm];
        await postJSON("/api/queue/remove", { source: it.source, name: it.name }).then((q) => { queue = q; });
        renderQueueBadge();
        toast("已移出同步队列: " + it.name);
        openQueueModal();
      })
    );
  }

  function closeModal() {
    $("#modal").classList.add("hidden");
    currentItem = null;
  }

  // ---------- 动作 ----------

  async function postJSON(url, body) {
    const res = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!res.ok) throw new Error("HTTP " + res.status);
    return res.json();
  }

  function inQueue(item) {
    return queue.items.some((q) => q.source === item.source && q.name === item.name);
  }

  function renderQueueBadge() {
    $("#queue-count").textContent = queue.items.length;
    $("#queue-count").classList.toggle("warn", queue.items.length > 0);
  }

  let toastTimer = null;
  function toast(msg, ok = true) {
    const t = $("#toast");
    t.textContent = msg;
    t.className = "toast " + (ok ? "ok" : "err");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => t.classList.add("hidden"), 2600);
  }

  // 从 brew 等命令输出中提取真正有用的错误行（Error: …）
  function extractErr(res) {
    let msg = res.error || "未知错误";
    if (res.output) {
      const lines = res.output.split("\n").map((l) => l.trim()).filter(Boolean);
      const errLine = lines.find((l) => l.includes("Error:"));
      if (errLine) msg += " · " + errLine;
      else if (lines.length) msg += " · " + lines[lines.length - 1];
    }
    return msg.length > 200 ? msg.slice(0, 197) + "…" : msg;
  }

  async function rescan() {
    const btn = $("#refresh");
    btn.disabled = true;
    btn.textContent = "盘点中…";
    try {
      report = await postJSON("/api/report", {});
      render();
      toast("盘点完成");
    } catch (err) {
      toast("盘点失败: " + err.message, false);
    } finally {
      btn.disabled = false;
      btn.textContent = "↻ 重新盘点";
    }
  }

  async function loadQueue() {
    const res = await fetch("/api/queue");
    queue = await res.json();
    if (!Array.isArray(queue.items)) queue.items = [];
    renderQueueBadge();
  }

  // ---------- 事件绑定 ----------

  $("#modal-cancel").addEventListener("click", closeModal);

  $("#modal-queue").addEventListener("click", async () => {
    if (!currentItem) return;
    const item = currentItem;
    try {
      if (inQueue(item)) {
        queue = await postJSON("/api/queue/remove", { source: item.source, name: item.name });
        toast("已移出同步队列: " + item.name);
      } else {
        queue = await postJSON("/api/queue", { source: item.source, name: item.name });
        toast("已加入同步队列: " + item.name);
      }
      renderQueueBadge();
      closeModal();
    } catch (err) {
      toast("操作失败: " + err.message, false);
    }
  });

  $("#modal-uninstall").addEventListener("click", async () => {
    if (!currentItem) return;
    const item = currentItem;
    if (!window.confirm(`确定卸载「${item.name}」吗？\n来源: ${item.source}\n此操作不可撤销。`)) return;
    const btn = $("#modal-uninstall");
    btn.disabled = true;
    btn.textContent = "卸载中…";
    try {
      const res = await postJSON("/api/uninstall", { source: item.source, name: item.name });
      if (res.ok) {
        toast("已卸载: " + item.name);
        closeModal();
        await rescan();
      } else {
        toast("卸载失败: " + extractErr(res), false);
      }
    } catch (err) {
      toast("卸载请求失败: " + err.message, false);
    } finally {
      btn.disabled = false;
      btn.textContent = "卸载";
    }
  });

  $("#queue-btn").addEventListener("click", openQueueModal);

  $("#refresh").addEventListener("click", async () => {
    content.innerHTML = `<div class="loading">正在重新盘点本机（brew / uv / 运行时 / 应用 / 配置）…</div>`;
    await rescan();
  });

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
      document.querySelectorAll("details.deps").forEach((d) => { d.open = !!q; });
    }, 120);
  });

  // 点击弹窗遮罩关闭
  $("#modal").addEventListener("click", (e) => {
    if (e.target === $("#modal")) closeModal();
  });

  // ---------- 初始加载 ----------
  Promise.all([
    fetch("/api/report").then((r) => (r.ok ? r.json() : Promise.reject(new Error("HTTP " + r.status)))),
    fetch("/api/queue").then((r) => r.json()),
  ])
    .then(([rep, q]) => { report = rep; queue = q; if (!Array.isArray(queue.items)) queue.items = []; render(); renderQueueBadge(); })
    .catch((err) => {
      content.innerHTML = `<div class="loading">加载失败: ${esc(err.message)}<br><br>
        <span style="color:var(--muted)">试试点右上角「重新盘点」</span></div>`;
    });
})();
