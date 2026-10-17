/* =============================================================================
 * Games Manager · TV 端主应用
 * 三个页面：Discover（Spotlight + 滑轨）/ Browse（网格 + 筛选）/ Detail（详情 + 安装）
 * 全手柄驱动：✕ 确认 · ○ 返回 · △ 筛选 · □ 收藏 · L1/R1 切换大类
 * ========================================================================== */

import { InputHub, FocusController, playSound, preloadSounds, isMuted, toggleMute } from "./tv-core.js";

/* ---------------------------------------------------------------- 状态 ---- */

const state = {
  route: "discover", // discover | browse | detail
  games: [],
  spotlight: [],
  tasks: [],
  filter: { platform: "ALL", genre: "ALL" },
  detail: null,
  favorites: new Set(JSON.parse(localStorage.getItem("tv:favorites") || "[]")),
  ps5ip: localStorage.getItem("tv:ps5ip") || "",
  drawerOpen: false,
  history: [],
};

const ROUTES = ["discover", "browse", "favorites"];

const $ = (sel) => document.querySelector(sel);
const el = {
  view: $("#view"),
  backdrop: $("#backdropImg"),
  hints: $("#hints"),
  tabs: $("#tabs"),
  toast: $("#toastWrap"),
  clock: $("#clock"),
  topbar: $("#topbar"),
};

/* ------------------------------------------------------------ 工具函数 ---- */

const esc = (s) =>
  String(s ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");

function formatSize(bytes) {
  if (!bytes) return "—";
  const gb = bytes / 1024 ** 3;
  if (gb >= 1) return gb.toFixed(gb >= 10 ? 0 : 1) + " GB";
  return Math.max(1, Math.round(bytes / 1024 ** 2)) + " MB";
}

function toast(msg, kind = "") {
  const node = document.createElement("div");
  node.className = "toast" + (kind ? " is-" + kind : "");
  node.textContent = msg;
  el.toast.appendChild(node);
  setTimeout(() => {
    node.style.opacity = "0";
    node.style.transform = "translateY(.5rem)";
    setTimeout(() => node.remove(), 300);
  }, 2600);
}

async function api(path, opts) {
  const res = await fetch(path, opts);
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    const err = new Error(text || res.statusText);
    err.status = res.status;
    throw err;
  }
  return res.json();
}

/* --------------------------------------------------------------- 焦点 ---- */

let lastScope = "";

const fc = new FocusController({
  onFocus: (node) => {
    // 焦点落在卡片/Spotlight 项时，切换全屏背景
    const bg = node.dataset.backdrop;
    if (bg) setBackdrop(bg, node.dataset.backdropCover === "1");
    // Spotlight 列表项聚焦 → 主视觉跟着切换
    if (node.classList.contains("spotlight-item")) showSpotlight(node.dataset.id);

    // 在滑轨之间纵向跳转时用「翻页」音效，与同一行内移动区分开
    const scope = node.dataset.scope || "";
    if (lastScope.startsWith("rail-") && scope.startsWith("rail-") && scope !== lastScope) {
      playSound("page");
    } else {
      playSound("focus");
    }
    lastScope = scope;
  },
  onBlocked: () => playSound("error"),
});

function setBackdrop(url, isCover) {
  if (!url) return;
  const img = el.backdrop;
  img.style.backgroundImage = `url("${url}")`;
  // 没有 16:9 横版素材时，用竖版封面做模糊填充，避免硬裁导致画面破碎
  img.classList.toggle("is-cover", !!isCover);
  img.classList.add("is-on");
}

/* --------------------------------------------------------------- 路由 ---- */

// 用 replaceState 同步地址栏，支持深链：#browse / #game/<title_id> / #discover
function syncHash(hash) {
  try {
    history.replaceState(null, "", "#" + hash);
  } catch {
    /* 忽略 */
  }
}

function parseHash() {
  const h = location.hash.replace(/^#\/?/, "");
  if (h.startsWith("game/")) return { route: "detail", id: decodeURIComponent(h.slice(5)) };
  if (h === "browse" || h === "discover" || h === "favorites") return { route: h };
  return { route: "discover" };
}

function go(route, payload) {
  if (state.route !== route) state.history.push(state.route);
  state.route = route;
  syncHash(route);
  render();
  if (payload) payload();
}

function back() {
  if (state.drawerOpen) return closeDrawer();
  if (state.route === "detail") {
    state.route = "browse";
    syncHash("browse");
    render();
    playSound("back");
    return true;
  }
  if (state.route === "browse" || state.route === "favorites") {
    state.route = "discover";
    syncHash("discover");
    render();
    playSound("back");
    return true;
  }
  return false;
}

/* ------------------------------------------------------------- 数据加载 ---- */

async function loadCatalog() {
  const data = await api("/api/tv/catalog");
  state.games = data.games || [];
  if (state.spotlight.length === 0) {
    const sp = await api("/api/tv/spotlight");
    state.spotlight = sp.games || [];
  }
}

/* --------------------------------------------------------------- 渲染 ---- */

function render() {
  if (state.route === "discover") renderDiscover();
  else if (state.route === "browse") renderBrowse();
  else if (state.route === "favorites") renderFavorites();
  else if (state.route === "detail") renderDetail();
  updateTabs();
  renderHints();
}

// L1 / R1 在三个大类之间循环
function cycleRoute(step) {
  const cur = state.route === "detail" ? "browse" : state.route;
  let i = ROUTES.indexOf(cur);
  if (i < 0) i = 0;
  i = (i + step + ROUTES.length) % ROUTES.length;
  go(ROUTES[i]);
  playSound("page");
}

function updateTabs() {
  el.tabs.querySelectorAll(".tab").forEach((t) => {
    const active =
      t.dataset.nav === state.route || (state.route === "detail" && t.dataset.nav === "browse");
    t.classList.toggle("is-active", active);
  });
}

/* ---------- Discover ---------- */

let spotlightIndex = 0;

function renderDiscover() {
  const list = state.spotlight.length ? state.spotlight : state.games.slice(0, 8);
  if (!list.length) {
    el.view.innerHTML = `<div class="empty">游戏库还是空的。请先在管理端「重新扫描」导入游戏。</div>`;
    fc.reset();
    return;
  }
  spotlightIndex = 0;
  const hero = list[0];

  const rail = (title, items, scope) => {
    if (!items.length) return "";
    return `
      <section class="rail">
        <div class="rail-head"><h2 class="rail-title">${esc(title)}</h2><span class="rail-more">${items.length} 款</span></div>
        <div class="rail-track" data-track="${scope}">
          ${items.map((g) => cardHTML(g, scope)).join("")}
        </div>
      </section>`;
  };

  const byRating = [...state.games].sort((a, b) => b.rating - a.rating).slice(0, 14);
  const byYear = [...state.games].sort((a, b) => b.year - a.year).slice(0, 14);
  // 每个平台各自成轨，保证 PS3 / Switch 也能在首页出现
  const platformRails = platformStats()
    .filter((p) => p.count >= 2)
    .map((p) => ({
      title: p.name === "PS3" ? "PS3 经典" : p.name + " 专区",
      items: state.games.filter((g) => (g.platform || "").toUpperCase() === p.name).slice(0, 14),
      scope: "rail-" + p.name.toLowerCase(),
    }));

  el.view.innerHTML = `
    <section class="spotlight">
      <div class="spotlight-hero" data-focus="hero" data-scope="spotlight" data-action="open" data-id="${esc(hero.id)}" data-backdrop="${esc(hero.backdrop_url)}" data-backdrop-cover="${hero.has_backdrop ? "" : "1"}">
        <div class="spotlight-art${hero.has_backdrop ? "" : " is-cover"}" id="spotArt" style="background-image:url('${esc(hero.backdrop_url)}')"></div>
        <div class="spotlight-scrim"></div>
        ${hero.has_backdrop ? "" : `<img class="spotlight-poster" id="spotPoster" src="${esc(hero.cover_url)}" alt="">`}
        <div class="spotlight-body">
          <h1 class="spotlight-title" id="spotTitle">${esc(hero.title)}</h1>
          <div class="spotlight-meta" id="spotMeta">${metaLine(hero)}</div>
          <p class="spotlight-summary" id="spotSummary">${esc(hero.developer || "")}</p>
          <div class="spotlight-actions">
            <button class="btn is-primary" data-focus="hero-play" data-action="open" data-id="${esc(hero.id)}">查看详情</button>
            <button class="btn" data-focus="hero-install" data-action="install" data-id="${esc(hero.id)}">安装到 PS5</button>
          </div>
        </div>
      </div>
      <div class="spotlight-list">
        ${list
          .map(
            (g, i) => `
          <button class="spotlight-item${i === 0 ? " is-current" : ""}" data-focus="spot-${esc(g.id)}" data-scope="spotlight"
                  data-action="spot" data-id="${esc(g.id)}" data-backdrop="${esc(g.backdrop_url)}" data-backdrop-cover="${g.has_backdrop ? "" : "1"}">
            <img class="spotlight-thumb" src="${esc(g.backdrop_url || g.cover_url)}" alt="">
            <div>
              <div class="spotlight-item-title">${esc(g.title)}</div>
              <div class="spotlight-item-sub">${esc(g.category)} · ${g.year || "—"}</div>
            </div>
          </button>`
          )
          .join("")}
      </div>
    </section>
    ${rail("高分推荐", byRating, "rail-rating")}
    ${rail("最新入库", byYear, "rail-recent")}
    ${platformRails.map((r) => rail(r.title, r.items, r.scope)).join("")}
  `;

  setBackdrop(hero.backdrop_url, !hero.has_backdrop);
  bindImages();
  fc.reset('[data-focus="hero"]');
}

function showSpotlight(id) {
  const g = state.spotlight.find((x) => x.id === id);
  if (!g) return;
  const art = document.getElementById("spotArt");
  if (!art) return;
  art.classList.add("is-fading");
  setTimeout(() => {
    art.style.backgroundImage = `url("${g.backdrop_url}")`;
    art.classList.toggle("is-cover", !g.has_backdrop);
    art.classList.remove("is-fading");
  }, 140);
  const poster = document.getElementById("spotPoster");
  if (poster) {
    if (g.has_backdrop) poster.remove();
    else poster.src = g.cover_url;
  }
  const t = document.getElementById("spotTitle");
  const m = document.getElementById("spotMeta");
  const s = document.getElementById("spotSummary");
  if (t) t.textContent = g.title;
  if (m) m.innerHTML = metaLine(g);
  if (s) s.textContent = g.developer || "";
  document.querySelectorAll(".spotlight-item").forEach((n) => n.classList.toggle("is-current", n.dataset.id === id));
  setBackdrop(g.backdrop_url, !g.has_backdrop);
}

function metaLine(g) {
  const parts = [];
  if (g.rating) parts.push(`<span class="badge">${g.rating}</span>`);
  if (g.year) parts.push(String(g.year));
  if (g.category) parts.push(esc(g.category));
  parts.push(formatSize(g.size_bytes));
  return parts.join(" · ");
}

function cardHTML(g, scope) {
  const fav = state.favorites.has(g.id);
  const platClass = g.platform === "PS5" ? "is-ps5" : g.platform === "SWITCH" ? "is-switch" : "";
  const progress = progressFor(g.id);
  return `
    <div class="card" data-focus="card-${esc(g.id)}" data-scope="${esc(scope)}" data-action="open"
         data-id="${esc(g.id)}" data-backdrop="${esc(g.backdrop_url)}" data-backdrop-cover="${g.has_backdrop ? "" : "1"}">
      <div class="card-tile">
        <img src="${esc(g.cover_url)}" alt="">
        <div class="card-fallback" hidden>${esc(g.title)}</div>
        <div class="card-glare"></div>
        <div class="card-shine"></div>
        <div class="card-badge">
          ${fav ? '<span class="badge">♥</span>' : ""}
          ${g.platform ? `<span class="badge ${platClass}">${esc(g.platform)}</span>` : ""}
        </div>
        ${progress !== null ? `<div class="card-progress"><i style="width:${progress}%"></i></div>` : ""}
      </div>
      <div class="card-caption">
        <div class="card-title">${esc(g.title)}</div>
        <div class="card-sub">${g.year || ""}${g.category ? " · " + esc(g.category) : ""}</div>
      </div>
    </div>`;
}

/* ---------- 平台统计 ---------- */

// 按实际数据统计各平台数量，避免硬编码平台列表与真实库不一致
function platformStats() {
  const order = ["PS5", "PS4", "PS3", "SWITCH"];
  const counts = new Map();
  for (const g of state.games) {
    const p = (g.platform || "OTHER").toUpperCase();
    counts.set(p, (counts.get(p) || 0) + 1);
  }
  const known = order.filter((p) => counts.has(p));
  const extra = [...counts.keys()].filter((p) => !order.includes(p)).sort();
  return [...known, ...extra].map((p) => ({ name: p, count: counts.get(p) }));
}

/* ---------- Browse ---------- */

function renderBrowse() {
  const stats = platformStats();
  const platformNames = new Set(stats.map((p) => p.name));
  const platforms = [{ name: "ALL", count: state.games.length }, ...stats];

  const genreSet = new Set();
  for (const g of state.games) {
    const c = g.category;
    if (!c || platformNames.has(c)) continue; // 与平台名重复的「类型」不单独成芯片
    genreSet.add(c);
  }
  const genres = ["ALL", ...[...genreSet].slice(0, 8)];

  const filtered = state.games.filter((g) => {
    if (state.filter.platform !== "ALL" && g.platform !== state.filter.platform) return false;
    if (state.filter.genre !== "ALL" && g.category !== state.filter.genre) return false;
    return true;
  });

  el.view.innerHTML = `
    <h1 class="page-title">游戏库</h1>
    <div class="filterbar" data-scope="filters">
      ${platforms
        .map(
          (p) =>
            `<button class="chip${state.filter.platform === p.name ? " is-active" : ""}" data-focus="pf-${p.name}"
                     data-scope="filters" data-action="filter" data-kind="platform" data-value="${p.name}">${
              p.name === "ALL" ? "全部平台" : p.name
            }<span class="chip-count">${p.count}</span></button>`
        )
        .join("")}
      ${genres
        .slice(1)
        .map(
          (g) =>
            `<button class="chip${state.filter.genre === g ? " is-active" : ""}" data-focus="gn-${esc(g)}"
                     data-scope="filters" data-action="filter" data-kind="genre" data-value="${esc(g)}">${esc(g)}</button>`
        )
        .join("")}
    </div>
    ${
      filtered.length
        ? `<div class="grid">${filtered.map((g) => cardHTML(g, "grid")).join("")}</div>`
        : `<div class="empty">没有符合条件的游戏。</div>`
    }
  `;

  bindImages();
  fc.reset(filtered.length ? ".grid .card" : '.filterbar .chip');
}

/* ---------- Favorites ---------- */

function renderFavorites() {
  const list = state.games.filter((g) => state.favorites.has(g.id));
  el.view.innerHTML = `
    <h1 class="page-title">我的收藏</h1>
    ${
      list.length
        ? `<div class="grid">${list.map((g) => cardHTML(g, "grid")).join("")}</div>`
        : `<div class="empty">还没有收藏。在任意卡片上按 □（键盘 F）即可加入收藏。</div>`
    }
  `;
  bindImages();
  updateTabs();
  renderHints();
  fc.reset(list.length ? ".grid .card" : null);
}

/* ---------- Detail ---------- */

async function openDetail(id) {
  state.detail = null;
  state.route = "detail";
  syncHash("game/" + encodeURIComponent(id));
  el.view.innerHTML = `<div class="empty">正在载入…</div>`;
  try {
    const page = await api("/api/tv/pages/" + encodeURIComponent(id));
    state.detail = page;
    renderDetail();
  } catch (e) {
    toast("读取详情失败：" + e.message, "err");
    state.route = "browse";
    syncHash("browse");
    render();
  }
}

function renderDetail() {
  const p = state.detail;
  if (!p) return;
  const g = p.game;
  setBackdrop(g.backdrop_url, !g.has_backdrop);

  const facts = [];
  if (p.release_date) facts.push(`<span><b>发售</b>${esc(p.release_date)}</span>`);
  if (p.developer) facts.push(`<span><b>开发</b>${esc(p.developer)}</span>`);
  if (p.publisher) facts.push(`<span><b>发行</b>${esc(p.publisher)}</span>`);
  if (p.rating) facts.push(`<span><b>评分</b>${Math.round(p.rating)}</span>`);
  facts.push(`<span><b>容量</b>${formatSize(g.size_bytes)}</span>`);

  el.view.innerHTML = `
    <div class="detail">
      <div class="detail-head">
        <img class="detail-cover" src="${esc(g.cover_url)}" alt="">
        <div class="detail-info">
          <h1 class="detail-title">${esc(g.title)}</h1>
          ${g.platform ? `<div class="detail-official">${esc(g.platform)} · ${esc(g.id)}</div>` : ""}
          <div class="detail-facts">${facts.join("")}</div>
          <div class="detail-genres">${(g.genres || []).map((x) => `<span class="genre-chip">${esc(x)}</span>`).join("")}</div>
          <div class="detail-actions">
            <button class="btn is-primary" data-focus="act-install-all" data-action="install" data-id="${esc(g.id)}" data-pkgtype="all">安装全部到 PS5</button>
            <button class="btn" data-focus="act-install-base" data-action="install" data-id="${esc(g.id)}" data-pkgtype="base">仅本体</button>
            ${p.patch_count ? `<button class="btn" data-focus="act-install-patch" data-action="install" data-id="${esc(g.id)}" data-pkgtype="patch">仅补丁</button>` : ""}
            <button class="btn" data-focus="act-back" data-action="back">返回</button>
          </div>
        </div>
      </div>

      ${
        p.screenshots && p.screenshots.length
          ? `<section>
        <h3 class="section-label">官方截图</h3>
        <div class="shots">
          ${p.screenshots
            .slice(0, 10)
            .map(
              (_, i) =>
                `<img class="shot" src="/api/v1/games/${p.game_id}/art?kind=screenshot&i=${i}" alt="" loading="lazy">`
            )
            .join("")}
        </div>
      </section>`
          : ""
      }

      <section>
        <h3 class="section-label">游戏简介</h3>
        <p class="detail-summary">${esc(p.summary || "暂无简介。可在游戏目录放置 gamelist.xml，或配置元数据源后重新刮削。")}</p>
      </section>

      <section>
        <h3 class="section-label">包含的包（${p.packages.length}）</h3>
        <div class="pkg-list">
          ${p.packages
            .map(
              (k) => `
            <div class="pkg" data-focus="pkg-${k.id}" data-scope="pkgs">
              <span class="pkg-kind">${k.kind === "base" ? "本体" : k.kind === "patch" ? "补丁" : "DLC"}</span>
              <span class="pkg-name">${esc(k.title)}</span>
              <span class="pkg-size">${formatSize(k.size_bytes)}</span>
            </div>`
            )
            .join("")}
        </div>
      </section>
    </div>
  `;

  bindImages();
  updateTabs();
  renderHints();
  fc.reset('[data-focus="act-install-all"]');
}

/* --------------------------------------------------------------- 交互 ---- */

function bindImages() {
  el.view.querySelectorAll("img").forEach((img) => {
    img.addEventListener("error", () => {
      img.style.display = "none";
      const fb = img.parentElement?.querySelector(".card-fallback");
      if (fb) fb.hidden = false;
    });
  });
}

async function handleAction(node, action) {
  const id = node.dataset.id;
  switch (action) {
    case "open":
      playSound("confirm");
      openDetail(id);
      return true;
    case "spot":
      playSound("confirm");
      openDetail(id);
      return true;
    case "back":
      playSound("back");
      back();
      return true;
    case "filter": {
      playSound("confirm");
      const kind = node.dataset.kind;
      const value = node.dataset.value;
      state.filter[kind] = value;
      const keep = node.dataset.focus;
      renderBrowse();
      fc.focus(document.querySelector(`[data-focus="${CSS.escape(keep)}"]`), { silent: true });
      return true;
    }
    case "install":
      playSound("confirm");
      install(id, node.dataset.pkgtype || "all");
      return true;
    default:
      return false;
  }
}

async function install(titleId, pkgType) {
  if (!titleId) return;
  toast("正在准备安装…");
  try {
    let ip = state.ps5ip;
    if (!ip) {
      toast("正在发现局域网内的 PS5…");
      try {
        const d = await api("/api/v1/devices");
        const list = d.data || d.devices || [];
        if (list.length) {
          ip = list[0].ip || list[0].address || "";
          if (ip) {
            state.ps5ip = ip;
            localStorage.setItem("tv:ps5ip", ip);
          }
        }
      } catch {
        /* 发现失败时交给服务端决定 */
      }
    }
    const res = await api("/api/tv/install", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title_id: titleId, package_type: pkgType, ps5_ip: ip }),
    });
    playSound("notify");
    toast(`已下发 ${res.queued ?? res.total ?? 1} 个安装任务`, "ok");
    pollTasks();
  } catch (e) {
    playSound("error");
    let msg = "安装失败";
    try {
      const j = JSON.parse(e.message);
      if (j.error === "ps5_not_configured") msg = "未找到 PS5：请确认主机已开机且与服务器在同一局域网";
      else if (j.message) msg = j.message;
    } catch {
      if (e.status === 404) msg = "找不到该游戏的文件";
    }
    toast(msg, "err");
  }
}

/* ----------------------------------------------------------- 安装进度 ---- */

function progressFor(titleId) {
  const t = state.tasks.find((x) => x.game_title && state.games.find((g) => g.id === titleId && g.title === x.game_title));
  if (!t || !t.total) return null;
  return Math.round(((t.done + t.failed) / t.total) * 100);
}

let taskTimer = null;
async function pollTasks() {
  if (taskTimer) return;
  const tick = async () => {
    try {
      const d = await api("/api/tv/tasks");
      state.tasks = d.tasks || [];
      const active = state.tasks.some((t) => t.state === "running" || t.state === "pending");
      if (state.route !== "detail") refreshProgressBars();
      if (!active) {
        clearInterval(taskTimer);
        taskTimer = null;
      }
    } catch {
      clearInterval(taskTimer);
      taskTimer = null;
    }
  };
  await tick();
  taskTimer = setInterval(tick, 1500);
}

function refreshProgressBars() {
  el.view.querySelectorAll(".card").forEach((c) => {
    const id = c.dataset.id;
    const pct = progressFor(id);
    if (pct === null) return;
    const tile = c.querySelector(".card-tile");
    if (!tile) return;
    let bar = tile.querySelector(".card-progress");
    if (!bar) {
      bar = document.createElement("div");
      bar.className = "card-progress";
      bar.innerHTML = "<i></i>";
      tile.appendChild(bar);
    }
    bar.querySelector("i").style.width = pct + "%";
  });
}

/* --------------------------------------------------------------- 提示条 ---- */

function renderHints() {
  const cross = `<span class="hint"><span class="glyph">✕</span>选择</span>`;
  const circle = `<span class="hint"><span class="glyph">○</span>返回</span>`;
  const tri = `<span class="hint"><span class="glyph">△</span>筛选</span>`;
  const sq = `<span class="hint"><span class="glyph">□</span>收藏</span>`;
  const lr = `<span class="hint"><span class="glyph is-cap">L1</span><span class="glyph is-cap">R1</span>切换</span>`;
  const opt = `<span class="hint"><span class="glyph is-cap">M</span>静音</span>`;

  if (state.route === "detail") {
    el.hints.innerHTML = cross.replace("选择", "安装 / 详情") + circle + opt;
  } else if (state.route === "favorites") {
    el.hints.innerHTML = cross + sq.replace("收藏", "取消收藏") + circle + lr + opt;
  } else {
    el.hints.innerHTML = cross + tri + sq + circle + lr + opt;
  }
}

/* --------------------------------------------------------------- 抽屉 ---- */

function openDrawer() {
  state.drawerOpen = true;
  const scrim = document.createElement("div");
  scrim.className = "drawer-scrim is-open";
  scrim.id = "drawerScrim";
  const drawer = document.createElement("aside");
  drawer.className = "drawer is-open";
  drawer.id = "drawer";
  const genres = [...new Set(state.games.map((g) => g.category).filter(Boolean))];
  drawer.innerHTML = `
    <h3>筛选</h3>
    <div class="drawer-group"><span>平台</span>
      ${["ALL", "PS5", "PS4", "PS3", "SWITCH"]
        .map(
          (p) =>
            `<button class="chip${state.filter.platform === p ? " is-active" : ""}" data-focus="dw-pf-${p}"
                     data-scope="drawer" data-action="filter" data-kind="platform" data-value="${p}">${p === "ALL" ? "全部" : p}</button>`
        )
        .join("")}
    </div>
    <div class="drawer-group"><span>类型</span>
      ${["ALL", ...genres]
        .map(
          (g) =>
            `<button class="chip${state.filter.genre === g ? " is-active" : ""}" data-focus="dw-gn-${esc(g)}"
                     data-scope="drawer" data-action="filter" data-kind="genre" data-value="${esc(g)}">${g === "ALL" ? "全部" : esc(g)}</button>`
        )
        .join("")}
    </div>
  `;
  document.body.appendChild(scrim);
  document.body.appendChild(drawer);
  scrim.addEventListener("click", closeDrawer);
  fc.focus(drawer.querySelector("[data-focus]"), { silent: true });
  playSound("page");
}

function closeDrawer() {
  state.drawerOpen = false;
  document.getElementById("drawer")?.remove();
  document.getElementById("drawerScrim")?.remove();
  playSound("back");
  fc.reset();
}

/* --------------------------------------------------------------- 输入 ---- */

const hub = new InputHub(async (evt) => {
  if (evt.type === "connected") {
    toast("手柄已连接", "ok");
    return;
  }
  if (evt.type === "disconnected") {
    toast("手柄已断开 · 可用键盘操作");
    return;
  }
  if (evt.type === "mute") {
    const m = toggleMute();
    toast(m ? "已静音" : "已恢复音效");
    return;
  }

  if (evt.type === "move") {
    if (state.drawerOpen) return fc.move(evt.dir);
    fc.move(evt.dir);
    return;
  }

  if (evt.type === "press") {
    const node = fc.current;
    switch (evt.action) {
      case "cross": {
        if (!node) return;
        const action = node.dataset.action;
        if (action) return void handleAction(node, action);
        if (node.dataset.id) return void handleAction(node, "open");
      }
      case "circle":
        return void back();
      case "triangle":
        if (state.route === "browse") {
          state.drawerOpen ? closeDrawer() : openDrawer();
        }
        return;
      case "square": {
        if (!node) return;
        const id = node.dataset.id;
        if (!id) return;
        if (state.favorites.has(id)) state.favorites.delete(id);
        else state.favorites.add(id);
        localStorage.setItem("tv:favorites", JSON.stringify([...state.favorites]));
        playSound("confirm");
        toast(state.favorites.has(id) ? "已加入收藏" : "已取消收藏");
        if (state.route === "browse") renderBrowse();
        else if (state.route === "discover") renderDiscover();
        return;
      }
      case "l1":
        cycleRoute(-1);
        return;
      case "r1":
        cycleRoute(1);
        return;
    }
  }
});

/* 鼠标/触摸：点击卡片等价于 ✕ */
el.view.addEventListener("click", (e) => {
  const node = e.target.closest("[data-action]");
  if (node) handleAction(node, node.dataset.action);
});

el.tabs.addEventListener("click", (e) => {
  const t = e.target.closest("[data-nav]");
  if (t) go(t.dataset.nav);
});

/* --------------------------------------------------------------- 启动 ---- */

function tickClock() {
  const d = new Date();
  el.clock.textContent = `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}
tickClock();
setInterval(tickClock, 20000);

(async function boot() {
  try {
    await loadCatalog();
  } catch (e) {
    el.view.innerHTML = `<div class="empty">无法连接服务：${esc(e.message)}</div>`;
    return;
  }
  // 深链：#browse / #game/<title_id>
  const h = parseHash();
  if (h.route === "detail" && h.id) {
    openDetail(h.id);
  } else {
    state.route = h.route;
    render();
  }
  preloadSounds();
  pollTasks();
})();

// 浏览器前进/后退、手动改 hash 时同步页面（replaceState 不会触发本事件，故无循环）
window.addEventListener("hashchange", () => {
  const h = parseHash();
  if (h.route === "detail" && h.id) openDetail(h.id);
  else {
    state.route = h.route;
    render();
  }
});

// 供调试：控制台可查看当前状态
window.__tv = { state, fc };
