// macsync 前端：仪表盘 + 工具交互（卸载 / 同步队列）。
(function () {
  "use strict";

  // 离线模式：scan --html 导出的单文件快照（内嵌 __MACSYNC_REPORT__，无需服务器）
  const OFFLINE = typeof window.__MACSYNC_REPORT__ !== "undefined";

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

  // 安装量：>=1万 显示为 "3.8万"
  function fmtCount(n) {
    if (!n) return "";
    return n >= 10000 ? (n / 10000).toFixed(1) + "万" : n.toLocaleString();
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

  // ---------- GitHub 同步 ----------

  let syncCfg = { github_repo: "", last_sync: "" };

  function syncStatusBadge() {
    return `<span class="sync-status ${syncCfg.github_repo ? "on" : "off"}">${syncCfg.github_repo ? "已配置" : "未配置"}</span>`;
  }

  function showSyncResult(res) {
    const el = $("#sync-result");
    if (el) {
      el.textContent = res.message || "";
      el.className = "sync-result " + (res.ok ? "ok" : "err");
    }
  }

  // 未配置：填写仓库 + 连接指引
  function renderSyncSetup() {
    $("#modal-title").textContent = "GitHub 同步 · 首次设置";
    $("#modal-uninstall").classList.add("hidden");
    $("#modal-queue").classList.add("hidden");
    $("#modal-cancel").textContent = "关闭";
    $("#modal-body").innerHTML = `
      <div class="sync-setup">
        <div class="hint">同步需要一个 GitHub <b>私有仓库</b>，存放同步队列与各机盘点快照（不含密钥配置）。</div>
        <label for="sync-repo-input">同步专用仓库（owner/name 或完整 URL）</label>
        <input id="sync-repo-input" placeholder="例如: peke/macsync-sync" value="${esc(syncCfg.github_repo)}">
        <div class="sync-hints">
          还没有仓库？在终端先执行：
          <code>gh auth login</code> 登录 GitHub
          <code>gh repo create macsync-sync --private</code> 创建私有仓库
        </div>
        <div class="sync-buttons">
          <button id="sync-save" class="primary">保存并测试连接</button>
        </div>
        <pre id="sync-result" class="sync-result"></pre>
      </div>`;
    $("#sync-save").addEventListener("click", async () => {
      const repo = $("#sync-repo-input").value.trim();
      if (!repo) { showSyncResult({ ok: false, message: "请先填写仓库" }); return; }
      const btn = $("#sync-save");
      btn.disabled = true; btn.textContent = "测试中…";
      try {
        syncCfg = await postJSON("/api/config", { github_repo: repo });
        const res = await postJSON("/api/sync/test", {});
        showSyncResult(res);
        if (res.ok) { toast("GitHub 同步已配置并连接"); renderSyncOps(); }
        else { toast("连接失败", false); }
      } catch (err) {
        showSyncResult({ ok: false, message: "请求失败: " + err.message });
      } finally {
        btn.disabled = false; btn.textContent = "保存并测试连接";
      }
    });
    $("#modal").classList.remove("hidden");
  }

  // 已配置：状态 + 测试/推送/拉取
  function renderSyncOps() {
    $("#modal-title").textContent = "GitHub 同步";
    $("#modal-uninstall").classList.add("hidden");
    $("#modal-queue").classList.add("hidden");
    $("#modal-cancel").textContent = "关闭";
    $("#modal-body").innerHTML = `
      <div class="item-detail">
        <div class="row"><span class="k">仓库</span><span class="v mono">${esc(syncCfg.github_repo)}</span></div>
        <div class="row"><span class="k">状态</span><span class="v">${syncStatusBadge()}</span></div>
        <div class="row"><span class="k">上次同步</span><span class="v mono" id="sync-last">${syncCfg.last_sync ? fmtTime(syncCfg.last_sync) : "从未"}</span></div>
      </div>
      <div class="sync-buttons">
        <button id="sync-test">测试连接</button>
        <button id="sync-push" class="primary">↑ 推送本机数据</button>
        <button id="sync-pull" class="primary">↓ 拉取远端数据</button>
        <button id="sync-reconfig" class="ghost">更换仓库</button>
      </div>
      <pre id="sync-result" class="sync-result"></pre>`;
    $("#sync-test").addEventListener("click", async () => {
      const btn = $("#sync-test"); btn.disabled = true;
      try { showSyncResult(await postJSON("/api/sync/test", {})); }
      catch (err) { showSyncResult({ ok: false, message: "请求失败: " + err.message }); }
      finally { btn.disabled = false; }
    });
    $("#sync-push").addEventListener("click", async () => runSync("push"));
    $("#sync-pull").addEventListener("click", async () => runSync("pull"));
    $("#sync-reconfig").addEventListener("click", renderSyncSetup);
    $("#modal").classList.remove("hidden");
  }

  async function runSync(direction) {
    const btn = direction === "push" ? $("#sync-push") : $("#sync-pull");
    btn.disabled = true;
    const orig = btn.textContent;
    btn.textContent = direction === "push" ? "推送中…" : "拉取中…";
    try {
      const res = await postJSON("/api/sync", { direction });
      showSyncResult(res);
      if (res.ok) {
        toast((direction === "push" ? "已推送" : "已拉取") + "到 GitHub");
        const cfg = await (await fetch("/api/config")).json();
        syncCfg = cfg;
        if (direction === "pull") { await loadQueue(); } // 远端队列可能合并进来
      } else {
        toast("同步失败", false);
      }
    } catch (err) {
      showSyncResult({ ok: false, message: "请求失败: " + err.message });
    } finally {
      btn.disabled = false;
      btn.textContent = orig;
      const lastEl = $("#sync-last");
      if (lastEl) lastEl.textContent = syncCfg.last_sync ? fmtTime(syncCfg.last_sync) : "从未";
    }
  }

  function openSyncModal() {
    $("#modal").classList.remove("hidden");
    $("#modal-title").textContent = "GitHub 同步";
    fetch("/api/config")
      .then((r) => r.json())
      .then((cfg) => { syncCfg = cfg; cfg.github_repo ? renderSyncOps() : renderSyncSetup(); })
      .catch(() => {
        $("#modal-body").innerHTML = `<div class="empty">加载配置失败</div>`;
      });
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
  $("#sync-btn").addEventListener("click", openSyncModal);
  $("#search-btn").addEventListener("click", openSearchView);

  $("#refresh").addEventListener("click", async () => {
    if (OFFLINE) {
      toast("这是静态快照，无法在线盘点。更新请重新运行: macsync scan --html <文件> --open", false);
      return;
    }
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

  // ---------- M2 搜索 ----------

  const SOURCE_LABEL = {
    "brew-formula": "formula",
    "brew-cask": "cask",
    "npm": "npm",
    "pypi": "PyPI",
  };

  let searchState = { q: "", results: [], notes: [], index: null, filter: "all", loading: false };

  function openSearchView() {
    content.innerHTML = `
      <div class="search-head">
        <input id="search-input" type="search" placeholder="搜索可安装的工具：brew formula / cask / npm / PyPI 包名…" value="${esc(searchState.q)}" autocomplete="off">
        <button id="search-go" class="primary">搜索</button>
        <button id="search-refresh" title="重新下载 Homebrew 索引（约 12MB）">刷新索引</button>
        <button id="search-back" class="ghost">← 返回盘点</button>
      </div>
      <div id="search-meta" class="hint"></div>
      <div id="search-filters" class="search-filters"></div>
      <div id="search-notes"></div>
      <div id="search-results"></div>`;

    $("#search-go").addEventListener("click", () => doSearch(false));
    $("#search-refresh").addEventListener("click", () => doSearch(true));
    $("#search-back").addEventListener("click", backToDashboard);
    $("#search-input").addEventListener("keydown", (e) => {
      if (e.key === "Enter") doSearch(false);
    });
    $("#search-input").focus();
    renderSearchState();
  }

  function backToDashboard() {
    if (report) { render(); return; }
    content.innerHTML = emptyStateHTML();
    const btn = $("#first-scan");
    if (btn) btn.addEventListener("click", firstScan);
  }

  async function doSearch(refresh) {
    const input = $("#search-input");
    const q = (input ? input.value : "").trim();
    if (!q) { toast("请输入搜索关键词", false); return; }
    const go = $("#search-go");
    searchState.q = q;
    searchState.loading = true;
    searchState.filter = "all";
    if (go) { go.disabled = true; go.textContent = "搜索中…"; }
    renderSearchState();
    try {
      const data = await (await fetch(`/api/search?q=${encodeURIComponent(q)}${refresh ? "&refresh=1" : ""}`)).json();
      searchState.results = data.results || [];
      searchState.notes = data.notes || [];
      searchState.index = data.index || null;
    } catch (err) {
      searchState.results = [];
      searchState.notes = ["搜索请求失败: " + err.message];
    } finally {
      searchState.loading = false;
      if (go) { go.disabled = false; go.textContent = "搜索"; }
      renderSearchState();
    }
  }

  function renderSearchState() {
    const meta = $("#search-meta");
    if (!meta) return;

    const idx = searchState.index;
    if (searchState.loading) {
      meta.innerHTML = idx && idx.count
        ? `<span class="spin">正在搜索…</span>`
        : `<span class="spin">首次搜索正在下载 Homebrew 索引（约 12MB），可能需要 10–60 秒…</span>`;
    } else if (idx && idx.count) {
      meta.innerHTML = `Homebrew 索引 ${idx.count.toLocaleString()} 条 · 更新于 ${idx.loaded_at ? fmtTime(idx.loaded_at) : "-"} · 结果 ${searchState.results.length} 条`;
    } else if (!searchState.q) {
      meta.innerHTML = `输入关键词开始搜索。<b>Homebrew</b> 用本地索引（首次约 12MB，之后毫秒级）；<b>npm</b> 实时搜索；<b>PyPI</b> 需精确包名。`;
    } else {
      meta.innerHTML = "";
    }

    $("#search-notes").innerHTML = (searchState.notes || [])
      .map((n) => `<div class="note">⚠️ ${esc(n)}</div>`)
      .join("");

    const counts = {};
    searchState.results.forEach((r) => { counts[r.source] = (counts[r.source] || 0) + 1; });
    const chips = [["all", `全部 (${searchState.results.length})`]].concat(
      Object.keys(counts).sort().map((s) => [s, `${SOURCE_LABEL[s] || s} (${counts[s]})`])
    );
    const filters = $("#search-filters");
    filters.innerHTML = searchState.results.length
      ? chips.map(([k, label]) =>
          `<span class="chip clickable ${searchState.filter === k ? "active" : ""}" data-filter="${esc(k)}">${esc(label)}</span>`
        ).join("")
      : "";
    filters.querySelectorAll("[data-filter]").forEach((el) =>
      el.addEventListener("click", () => { searchState.filter = el.dataset.filter; renderSearchState(); })
    );

    const box = $("#search-results");
    let list = searchState.results;
    if (searchState.filter !== "all") list = list.filter((r) => r.source === searchState.filter);

    if (!list.length) {
      box.innerHTML = (!searchState.loading && searchState.q)
        ? `<div class="empty">没有找到「${esc(searchState.q)}」相关结果${
            SOURCE_LABEL ? "（PyPI 需精确包名，npm/brew 支持模糊搜索）" : ""
          }</div>`
        : "";
      return;
    }

    box.innerHTML = `<table><thead><tr><th>名称</th><th>版本</th><th>说明</th><th>来源 / 状态</th><th>操作</th></tr></thead><tbody>${
      list.map((r) => {
        const dep = r.deprecated ? '<span class="tag outdated">已废弃</span>' : "";
        const aliasTag = r.alias ? `<span class="tag dep">别名 ${esc(r.alias)}</span>` : "";
        const label = r.display
          ? `${esc(r.display)} <small class="mono">${esc(r.name)}</small>`
          : esc(r.name);
        const status = r.installed
          ? '<span class="sync-status on">已安装</span>'
          : '<span class="sync-status off">未安装</span>';
        const pop = r.popular ? `<span class="pop">30天 ${fmtCount(r.popular)}</span>` : "";
        const queued = inQueue({ source: r.source, name: r.name });
        const sel = JSON.stringify({ source: r.source, name: r.name }).replace(/'/g, "&#39;");
        const actions = [
          r.installed ? "" : `<button class="small primary" data-install='${sel}'>安装</button>`,
          `<button class="small ${queued ? "danger-ghost" : ""}" data-queue='${sel}'>${queued ? "移出队列" : "入队"}</button>`,
        ].filter(Boolean).join(" ");
        return `<tr>
          <td class="name">${label}${aliasTag}${dep}</td>
          <td class="mono">${esc(r.version)}</td>
          <td class="desc">${esc(r.desc)}</td>
          <td><span class="tag dep">${esc(SOURCE_LABEL[r.source] || r.source)}</span> ${status} ${pop}</td>
          <td class="ops">${actions}</td>
        </tr>`;
      }).join("")
    }</tbody></table>`;

    bindSearchActions();
  }

  function bindSearchActions() {
    document.querySelectorAll("[data-install]").forEach((btn) =>
      btn.addEventListener("click", async () => {
        const item = JSON.parse(btn.dataset.install);
        if (!window.confirm(`安装「${item.name}」？\n来源: ${SOURCE_LABEL[item.source] || item.source}`)) return;
        btn.disabled = true;
        btn.textContent = "安装中…";
        try {
          const res = await postJSON("/api/install", item);
          if (res.ok) {
            toast("已安装: " + item.name + "（点「重新盘点」可刷新盘点数据）");
            searchState.results.forEach((r) => {
              if (r.source === item.source && r.name === item.name) r.installed = true;
            });
          } else {
            toast("安装失败: " + extractErr(res), false);
          }
        } catch (err) {
          toast("安装请求失败: " + err.message, false);
        } finally {
          btn.disabled = false;
          btn.textContent = "安装";
          renderSearchState();
        }
      })
    );
    document.querySelectorAll("[data-queue]").forEach((btn) =>
      btn.addEventListener("click", async () => {
        const item = JSON.parse(btn.dataset.queue);
        try {
          if (inQueue(item)) {
            queue = await postJSON("/api/queue/remove", item);
            toast("已移出同步队列: " + item.name);
          } else {
            queue = await postJSON("/api/queue", item);
            toast("已加入同步队列: " + item.name);
          }
          renderQueueBadge();
          renderSearchState();
        } catch (err) {
          toast("操作失败: " + err.message, false);
        }
      })
    );
  }

  // ---------- 初始加载 ----------
  if (OFFLINE) {
    document.body.classList.add("offline");
    report = window.__MACSYNC_REPORT__;
    queue = { items: [] };
    render();
    renderQueueBadge();
  } else {
    fetch("/api/report")
      .then(async (r) => {
        if (r.status === 404) {
          content.innerHTML = emptyStateHTML();
          const btn = $("#first-scan");
          if (btn) btn.addEventListener("click", firstScan);
          return null;
        }
        if (!r.ok) throw new Error("HTTP " + r.status);
        return r.json();
      })
      .then((rep) => {
        if (rep) { report = rep; render(); renderQueueBadge(); }
      })
      .catch((err) => {
        content.innerHTML = `<div class="loading">加载失败: ${esc(err.message)}<br><br>
          <span style="color:var(--muted)">试试点右上角「重新盘点」</span></div>`;
      });
  }

  function emptyStateHTML() {
    return `
      <div class="empty-state">
        <div class="empty-icon">🔍</div>
        <h2>还没有盘点数据</h2>
        <p>首次盘点约需 10–20 秒（brew 版本检查较慢）。<br>
           盘点结果会缓存为 <code>~/.macsync/current.json</code>，<br>
           <b>之后打开本页面直接读取缓存，不会重新扫描</b>；只有点「重新盘点」才会更新。</p>
        <button id="first-scan" class="primary">开始首次盘点</button>
      </div>`;
  }

  function firstScan() {
    content.innerHTML = `<div class="loading">首次盘点中（约 10–20 秒）…</div>`;
    rescan();
  }
})();
