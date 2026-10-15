package server

import (
	"net/http"
)

func (h *ServerHandler) handleWebIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 禁止浏览器缓存 index，确保代码改动立刻生效
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write([]byte(indexHTML))
}

const indexHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta name="theme-color" content="#07090e">
  <title>Games Manager</title>
  <!-- 自托管样式，完全摆脱 cdn.tailwindcss.com -->
  <link rel="stylesheet" href="/static/style.css?v=10">
  <link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'%3E%3Crect width='100' height='100' rx='20' fill='%230070d1'/%3E%3Ctext x='50' y='66' text-anchor='middle' font-size='52' font-weight='900' fill='white' font-family='sans-serif'%3EP%3C/text%3E%3C/svg%3E">
</head>
<body onclick="closeContextMenu()">

  <!-- 头部导航（两行：上行 品牌/搜索/设备，下行 平台/视图/操作） -->
  <header class="app-header">
    <div class="header-top">
      <div class="brand">Games Manager</div>
      <input id="searchInput" type="text" placeholder="搜索游戏名称、TitleID、地区..." class="search-input">

      <div class="device-area">
        <div class="device-pill">
          <select id="discoveredSelect" onchange="applySelectedHost(this.value)" class="device-select">
            <option value="">在线设备 (0)</option>
          </select>
          <button onclick="scanDevices()" id="scanDeviceBtn" title="重新扫描 PS5" class="icon-btn">
            <svg id="scanIcon" viewBox="0 0 24 24">
              <path d="M12 4V1L8 5l4 4V6c3.31 0 6 2.69 6 6 0 1.01-.25 1.97-.7 2.8l1.46 1.46A7.93 7.93 0 0 0 20 12c0-4.42-3.58-8-8-8zm0 14c-3.31 0-6-2.69-6-6 0-1.01.25-1.97.7-2.8L5.24 7.74A7.93 7.93 0 0 0 4 12c0 4.42 3.58 8 8 8v3l4-4-4-4v3z"/>
            </svg>
          </button>
        </div>
        <input id="ps5IpInput" type="text" placeholder="PS5 IP (如 192.168.1.100)" class="ps5-ip-input">
      </div>
    </div>

    <div class="header-bottom">
      <div class="filter-tabs">
        <button onclick="setFilter('PS5')" id="filter-PS5" class="filter-tab active">PS5</button>
        <button onclick="setFilter('PS4')" id="filter-PS4" class="filter-tab">PS4</button>
        <button onclick="setFilter('PS3')" id="filter-PS3" class="filter-tab">PS3</button>
        <button onclick="setFilter('SWITCH')" id="filter-SWITCH" class="filter-tab">Switch</button>
      </div>

      <div class="filter-tabs" id="viewToggle" title="切换卡片显示样式">
        <button onclick="setViewMode('cover')" id="view-cover" class="filter-tab active">封面</button>
        <button onclick="setViewMode('icon')" id="view-icon" class="filter-tab">图标</button>
      </div>

      <div class="header-actions">
        <button onclick="openMetaModal()" class="btn btn-secondary">元数据</button>
        <button onclick="openSettingsModal()" class="btn btn-secondary">目录设置</button>
        <button onclick="triggerScan()" class="btn btn-primary">重新扫描</button>
      </div>
    </div>
  </header>

  <!-- 主卡片网格 -->
  <main class="app-main">
    <div id="grid" class="game-grid"></div>
  </main>

  <!-- 隐藏的封面上传 Input -->
  <input type="file" id="coverFileInput" class="hidden" accept="image/png,image/jpeg,image/webp" onchange="handleCoverFileSelected(this)">

  <!-- 右键上下文菜单 -->
  <div id="contextMenu" class="context-menu hidden">
    <button onclick="menuShowDetail()" class="context-menu-item">📖 查看详情</button>
    <button onclick="menuScrapeGame()" class="context-menu-item">✨ 重新刮削元数据</button>
    <div class="context-menu-divider"></div>
    <button onclick="menuUploadCover()" class="context-menu-item">📤 更换封面</button>
    <button onclick="menuFetchOnlineCover()" class="context-menu-item">🌐 在线获取封面</button>
    <button onclick="menuDeleteCover()" class="context-menu-item">🗑️ 删除封面</button>
    <div class="context-menu-divider"></div>
    <button onclick="menuDeleteGame()" class="context-menu-item is-danger">🗑️ 删除游戏记录</button>
  </div>

  <!-- 游戏详情模态框（元数据） -->
  <div id="gameModal" class="modal-overlay is-top hidden" onclick="if(event.target===this)closeGameModal()">
    <div class="modal modal-lg detail-modal">
      <div class="detail-hero" id="detailHero">
        <div class="detail-hero-scrim"></div>
        <button onclick="closeGameModal()" class="modal-close detail-close">✕</button>
        <div class="detail-head">
          <img id="detailCover" class="detail-cover" alt="" src="">
          <div class="detail-headinfo">
            <div class="detail-badges" id="detailBadges"></div>
            <h2 id="detailNameCN" class="detail-name-cn"></h2>
            <div id="detailNameOfficial" class="detail-name-official"></div>
            <div id="detailFacts" class="detail-facts"></div>
            <div id="detailActions" class="detail-actions"></div>
          </div>
        </div>
      </div>
      <div class="detail-body">
        <h4 class="detail-section-title">游戏简介</h4>
        <div id="detailSummary" class="detail-summary"></div>
        <div id="detailShotsWrap">
          <h4 class="detail-section-title">官方截图</h4>
          <div id="detailShots" class="detail-shots"></div>
        </div>
      </div>
    </div>
  </div>

  <!-- 元数据刮削设置与进度 -->
  <div id="metaModal" class="modal-overlay hidden" onclick="if(event.target===this)closeMetaModal()">
    <div class="modal modal-md">
      <div class="modal-header">
        <h3><span>元数据刮削</span></h3>
        <button onclick="closeMetaModal()" class="modal-close">✕</button>
      </div>
      <div class="modal-body">
        <div class="meta-nokey">
          <span class="nokey-icon">🔓</span>
          <div>
            <b>无需任何 API Key 即可使用。</b><br>
            内置 5 个免 Key 数据源：<b>本地 gamelist.xml</b>（中文简介 / 中文译名）、
            <b>Bangumi 番组计划</b>（简体中文名 / 中文简介 / 开发商 / 发行商 / 评分）、
            <b>Switch TitleDB</b>（Switch 官方名称与素材）、
            <b>Hasheous</b> 与 <b>Playmatch</b>（PS3 镜像哈希匹配）。<br>
            <b>没有 IGDB 凭证也能拿到中文简介</b>——由 Bangumi 提供。<br>
            下方的 IGDB 与 SteamGridDB 属于<b>可选增强</b>（主要补充高清素材），留空不影响中文简介刮削。
          </div>
        </div>

        <div class="meta-status" id="metaStatusBox">正在读取状态…</div>

        <div class="modal-section">
          <label class="modal-label">数据源开关</label>
          <label class="meta-check"><input type="checkbox" id="metaEnabled"> 启用在线刮削</label>
          <label class="meta-check"><input type="checkbox" id="metaBangumi"> Bangumi 番组计划（免 Key，中文名 / 中文简介）</label>
          <label class="meta-check"><input type="checkbox" id="metaHasheous"> Hasheous 哈希匹配（免 Key，仅 PS3）</label>
          <label class="meta-check"><input type="checkbox" id="metaPlaymatch"> Playmatch 哈希匹配（免 Key，仅 PS3）</label>
          <label class="meta-check"><input type="checkbox" id="metaTitleDB"> Switch TitleDB（免 Key，首次下载约 90MB，缓存 7 天）</label>
        </div>

        <div class="modal-section">
          <label class="modal-label">IGDB<span class="opt">可选 · 官方元数据兜底</span></label>
          <div class="modal-input-row">
            <input id="metaIGDBId" type="text" placeholder="IGDB Client ID">
          </div>
          <div class="modal-input-row" style="margin-top:8px;">
            <input id="metaIGDBSecret" type="password" placeholder="IGDB Client Secret（留空表示不修改）">
          </div>
          <p class="field-hint">
            在 <a href="https://dev.twitch.tv/console/apps" target="_blank" rel="noopener">dev.twitch.tv/console/apps</a>
            注册应用（Category 选 Application Integration，Client Type 选 Confidential），
            名称需全局唯一，建议 <code>romm-&lt;随机串&gt;</code>。需要 Twitch 账号 + 手机 2FA。
          </p>
        </div>

        <div class="modal-section">
          <label class="modal-label">SteamGridDB<span class="opt">可选 · 高清封面 / Logo / Hero</span></label>
          <div class="modal-input-row">
            <input id="metaSGDBKey" type="password" placeholder="SteamGridDB API Key（留空表示不修改）">
          </div>
          <p class="field-hint">
            在 <a href="https://www.steamgriddb.com/profile/preferences/api" target="_blank" rel="noopener">steamgriddb.com/profile/preferences/api</a>
            登录后即可生成 API Key。
          </p>
        </div>

        <div class="modal-section">
          <label class="modal-label">语言 / 地区优先级（逗号分隔）</label>
          <div class="modal-input-row">
            <input id="metaLanguage" type="text" placeholder="zh,en">
          </div>
          <div class="modal-input-row" style="margin-top:8px;">
            <input id="metaRegion" type="text" placeholder="cn,us,eu,jp,wor">
          </div>
          <p class="field-hint">
            语言码取自 RomM 官方枚举：ar da de el en es fi fr it ja ko nl no pl pt ru sr sv zh；
            地区码：au asi br ca cn uk eu fi fr de gr nl hk it jp kr no ru sp se tw us wor 等。
          </p>
        </div>

        <div class="modal-section">
          <label class="modal-label">刮削任务</label>
          <div id="metaProgress" class="meta-progress hidden"></div>
          <div class="meta-actions">
            <button onclick="saveMetaSettings()" class="btn btn-secondary">保存设置</button>
            <button onclick="startScrape(false)" class="btn btn-primary">刮削缺失项</button>
            <button onclick="startScrape(true)" class="btn btn-ghost">全部重新刮削</button>
          </div>
        </div>
      </div>
      <div class="modal-actions">
        <button onclick="closeMetaModal()" class="btn btn-ghost">关闭</button>
      </div>
    </div>
  </div>

  <!-- DLC 详情模态框 -->
  <div id="dlcModal" class="modal-overlay is-top hidden" onclick="if(event.target===this)closeDLCModal()">
    <div class="modal modal-sm">
      <div class="modal-header">
        <div class="title-row">
          <span class="name" id="dlcModalGameTitle">游戏 DLC 清单</span>
          <span id="dlcModalTitleId" class="titleid"></span>
        </div>
        <button onclick="closeDLCModal()" class="modal-close">✕</button>
      </div>
      <div id="dlcModalList" class="modal-body dlc-list">
        <div class="empty-state">正在加载 DLC...</div>
      </div>
      <div class="dlc-summary">
        <div style="display:flex; justify-content:space-between; align-items:center;">
          <span id="dlcModalSummary">共 0 个追加内容</span>
          <button onclick="closeDLCModal()" class="btn btn-ghost">关闭</button>
        </div>
      </div>
    </div>
  </div>

  <!-- 安装进度模态框 -->
  <div id="installModal" class="modal-overlay is-top hidden" onclick="if(event.target===this)closeInstallModal()">
    <div class="modal modal-md">
      <div class="modal-header">
        <div class="title-row">
          <span class="name" id="installTitle">正在安装…</span>
        </div>
        <button onclick="closeInstallModal()" class="modal-close">✕</button>
      </div>
      <input type="hidden" id="installTaskId" value="">
      <div id="installList" class="modal-body install-list" style="max-height:52vh;">
        <div class="empty-state">正在下发安装任务…</div>
      </div>
      <div class="dlc-summary">
        <div style="display:flex; justify-content:space-between; align-items:center; gap:12px;">
          <span id="installSummary"></span>
          <button id="installCloseBtn" onclick="closeInstallModal()" class="btn btn-ghost" disabled>关闭</button>
        </div>
      </div>
    </div>
  </div>

  <!-- 目录设置弹窗 -->
  <div id="settingsModal" class="modal-overlay hidden">
    <div class="modal modal-md">
      <div class="modal-header">
        <h3>
          <span>游戏存储目录设置</span>
          <button onclick="addCustomDirInput('')" class="btn-add-dir">+ 添加目录</button>
        </h3>
      </div>
      <div class="modal-body">
        <div class="modal-section">
          <label class="modal-label">PS5 游戏目录</label>
          <div class="modal-input-row">
            <input id="modalPs5Dir" type="text" placeholder="/packages/ps5 或本地绝对路径">
            <button onclick="openFolderPicker('modalPs5Dir')" class="btn btn-secondary">选择路径</button>
          </div>
        </div>
        <div class="modal-section">
          <label class="modal-label">PS4 游戏目录</label>
          <div class="modal-input-row">
            <input id="modalPs4Dir" type="text" placeholder="/packages/ps4 或本地绝对路径">
            <button onclick="openFolderPicker('modalPs4Dir')" class="btn btn-secondary">选择路径</button>
          </div>
        </div>
        <div class="modal-section">
          <label class="modal-label">Switch 游戏目录</label>
          <div class="modal-input-row">
            <input id="modalSwitchDir" type="text" placeholder="/packages/switch 或本地绝对路径">
            <button onclick="openFolderPicker('modalSwitchDir')" class="btn btn-secondary">选择路径</button>
          </div>
        </div>
        <div id="customDirsContainer" class="custom-dirs"></div>
      </div>
      <div class="modal-actions">
        <button onclick="closeSettingsModal()" class="btn btn-ghost">取消</button>
        <button onclick="saveSettingsModal()" class="btn btn-primary">保存配置</button>
      </div>
    </div>
  </div>

  <!-- 文件夹选择器 Modal -->
  <div id="folderPickerModal" class="modal-overlay hidden">
    <div class="modal modal-lg folder-picker">
      <div class="modal-header">
        <h4><span>浏览选择服务器目录</span></h4>
        <button onclick="closeFolderPicker()" class="modal-close">✕</button>
      </div>

      <div class="folder-quick">
        <span>快速跳转:</span>
        <button type="button" onclick="loadPickerPath('/')">/ (根目录)</button>
        <button type="button" onclick="loadPickerPath('/packages')" class="blue">/packages</button>
        <button type="button" onclick="loadPickerPath('.')" class="green">当前目录</button>
      </div>

      <div class="folder-current" id="pickerCurrentPath">/</div>
      <div id="pickerList" class="folder-list">
        <div class="empty-state">正在加载目录...</div>
      </div>

      <div class="modal-actions" style="border-top: 1px solid var(--border-soft); padding-top: 12px;">
        <button onclick="pickerGoParent()" class="btn btn-ghost">上一级</button>
        <div style="display:flex; gap:8px;">
          <button onclick="closeFolderPicker()" class="btn btn-ghost">取消</button>
          <button onclick="pickerConfirm()" class="btn btn-primary">确认选择</button>
        </div>
      </div>
    </div>
  </div>

  <script>
    /* ============== 状态 ============== */
    let games = [];
    let progressMap = {};
    let lastRenderedJson = '';
    let currentFilter = 'PS5';
    let currentContextGameId = null;
    let targetInputIdForPicker = '';
    let pickerCurrent = '/';
    let pickerParent = '/';
    let loadTimestamp = Date.now();
    let scanInFlight = false;
    // 显示样式：'cover'（竖版封面）| 'icon'（方形图标）
    let viewMode = localStorage.getItem('ps5_view_mode') || 'cover';
    let installPollTimer = null;

    // Switch 官方只有方形图标，故 Switch 选项卡一律使用「图标」样式
    function effectiveViewMode() {
      return currentFilter === 'SWITCH' ? 'icon' : viewMode;
    }

    // 同步网格样式类 + 按钮高亮 + 切换器显隐
    function applyViewClasses() {
      const eff = effectiveViewMode();
      const grid = document.getElementById('grid');
      grid.classList.toggle('mode-cover', eff === 'cover');
      grid.classList.toggle('mode-icon', eff === 'icon');

      document.getElementById('view-cover').classList.toggle('active', eff === 'cover');
      document.getElementById('view-icon').classList.toggle('active', eff === 'icon');

      // Switch 选项卡下隐藏「封面/图标」切换器（只有图标可用）
      const toggle = document.getElementById('viewToggle');
      if (toggle) toggle.classList.toggle('hidden', currentFilter === 'SWITCH');
    }

    // 全局图片加载失败处理：用对应比例的占位 SVG 替代
    document.addEventListener('error', function(e) {
      const t = e.target;
      if (t && t.tagName === 'IMG' && t.classList.contains('cover-img')) {
        const card = t.closest('.ps-card');
        const title = card ? (card.dataset.title || 'GAME') : 'GAME';
        const platform = card ? (card.dataset.platform || 'PS5') : 'PS5';
        t.onerror = null;
        t.src = getFallbackSvg(title, platform, effectiveViewMode());
      }
    }, true);

    /* ============== 显示样式切换（封面 / 图标） ============== */
    function setViewMode(mode) {
      viewMode = (mode === 'icon') ? 'icon' : 'cover';
      localStorage.setItem('ps5_view_mode', viewMode);
      applyViewClasses();
      loadTimestamp = Date.now();
      render();
    }

    /* ============== 过滤 ============== */
    function setFilter(cat) {
      currentFilter = cat;
      ['PS5', 'PS4', 'PS3', 'SWITCH'].forEach(k => {
        const btn = document.getElementById('filter-' + k);
        btn.classList.toggle('active', k === cat);
      });
      // 切换平台时同步显示样式（Switch 强制图标模式）
      applyViewClasses();
      loadTimestamp = Date.now();
      render();
    }

    /* ============== 占位封面 SVG（封面 2:3 / 图标 1:1） ============== */
    function getFallbackSvg(title, platform, mode) {
      // 名称过长时截断，并按长度自适应字号，避免超出画布被裁切
      let name = String(title || 'GAME').trim();
      if (name.length > 28) name = name.slice(0, 27) + '…';
      const safe = name
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
      const isSw = platform === 'SWITCH';
      const strokeCol = isSw ? '#e60012' : '#0070d1';
      const textTag = isSw ? 'SWITCH' : (platform === 'PS5' ? 'PS5 GAME' : (platform === 'PS3' ? 'PS3 GAME' : 'PS4 GAME'));
      const square = (mode === 'icon');
      const W = 600;
      const H = square ? 600 : 900;
      // 依据名称长度自适应字号（可用宽度约 520px，单字符宽约 0.56 * 字号）
      const maxW = 520;
      let fs = Math.floor(maxW / Math.max(1, name.length) / 0.56);
      fs = Math.max(20, Math.min(square ? 40 : 46, fs));
      const y1 = square ? '46%' : '46%';
      const y2 = square ? '60%' : '58%';
      const svg =
        '<svg xmlns="http://www.w3.org/2000/svg" width="' + W + '" height="' + H + '" viewBox="0 0 ' + W + ' ' + H + '">' +
          '<defs>' +
            '<linearGradient id="bg" x1="0" y1="0" x2="0" y2="1">' +
              '<stop offset="0%" stop-color="#131926"/>' +
              '<stop offset="100%" stop-color="#0a0d14"/>' +
            '</linearGradient>' +
          '</defs>' +
          '<rect width="' + W + '" height="' + H + '" fill="url(#bg)"/>' +
          '<rect x="24" y="24" width="' + (W-48) + '" height="' + (H-48) + '" rx="20" fill="none" stroke="' + strokeCol + '" stroke-width="2" stroke-dasharray="8 8" opacity="0.4"/>' +
          '<text x="50%" y="' + y1 + '" dominant-baseline="middle" text-anchor="middle" fill="#ffffff" font-size="' + fs + '" font-family="-apple-system,sans-serif" font-weight="700">' + safe + '</text>' +
          '<text x="50%" y="' + y2 + '" dominant-baseline="middle" text-anchor="middle" fill="' + strokeCol + '" font-size="20" font-family="-apple-system,sans-serif" font-weight="bold" letter-spacing="4">' + textTag + '</text>' +
        '</svg>';
      return 'data:image/svg+xml;utf8,' + encodeURIComponent(svg);
    }

    /* ============== 数据加载 ============== */
    async function loadGames(search = '') {
      try {
        const res = await fetch('/api/v1/games?search=' + encodeURIComponent(search));
        const json = await res.json();
        const newGames = json.data || [];
        const currentJson = JSON.stringify(newGames);
        if (currentJson !== lastRenderedJson) {
          lastRenderedJson = currentJson;
          games = newGames;
          render();
        }
      } catch (e) {
        console.error('loadGames 失败:', e);
      }
    }

    async function scanDevices() {
      const select = document.getElementById('discoveredSelect');
      const icon = document.getElementById('scanIcon');
      if (icon) icon.classList.add('is-spinning');
      select.innerHTML = '<option value="">正在扫描...</option>';
      try {
        const res = await fetch('/api/v1/devices');
        const json = await res.json();
        const devs = json.data || [];
        select.innerHTML = '<option value="">在线设备 (' + devs.length + ')</option>';
        if (devs.length > 0) {
          devs.forEach(dev => {
            const opt = document.createElement('option');
            opt.value = dev.ip;
            opt.textContent = dev.ip + ':' + dev.port;
            select.appendChild(opt);
          });
          document.getElementById('ps5IpInput').value = devs[0].ip;
        } else {
          document.getElementById('ps5IpInput').value = '';
        }
      } catch (e) {
        select.innerHTML = '<option value="">扫描失败</option>';
      } finally {
        if (icon) icon.classList.remove('is-spinning');
      }
    }

    function applySelectedHost(val) {
      if (val) document.getElementById('ps5IpInput').value = val;
    }

    async function updateProgress() {
      try {
        const res = await fetch('/api/v1/progress');
        const json = await res.json();
        progressMap = {};
        if (json.data) {
          json.data.forEach(p => { progressMap[p.game_id] = p; });
        }
        renderProgressOnly();
      } catch (e) {}
    }

    function formatETA(secs) {
      if (!secs || secs <= 0) return '--';
      const m = Math.floor(secs / 60);
      const s = Math.floor(secs % 60);
      return m > 0 ? (m + 'm ' + s + 's') : (s + 's');
    }

    function renderProgressOnly() {
      games.forEach(g => {
        const container = document.getElementById('progress-wrap-' + g.id);
        if (!container) return;
        const p = progressMap[g.id];
        if (p) {
          container.classList.remove('hidden');
          container.innerHTML =
            '<div class="progress-bar">' +
            '  <div class="progress-fill" style="width: ' + p.percent.toFixed(1) + '%"></div>' +
            '</div>' +
            '<div class="progress-info">' +
            '  <span class="pct">' + p.percent.toFixed(1) + '% (' + p.speed_mbps.toFixed(1) + ' MB/s)</span>' +
            '  <span>剩余: ' + formatETA(p.eta_seconds) + '</span>' +
            '</div>';
        } else {
          container.classList.add('hidden');
        }
      });
    }

    /* ============== 右键菜单 ============== */
    function showCardContextMenu(e, gameId) {
      e.preventDefault();
      e.stopPropagation();
      currentContextGameId = gameId;
      const menu = document.getElementById('contextMenu');
      menu.style.left = e.clientX + 'px';
      menu.style.top = e.clientY + 'px';
      menu.classList.remove('hidden');
    }

    function closeContextMenu() {
      const menu = document.getElementById('contextMenu');
      if (menu) menu.classList.add('hidden');
    }

    function menuUploadCover() {
      closeContextMenu();
      const input = document.getElementById('coverFileInput');
      input.value = '';
      input.click();
    }

    async function handleCoverFileSelected(input) {
      if (!input.files || input.files.length === 0 || !currentContextGameId) return;
      const file = input.files[0];
      const formData = new FormData();
      formData.append('cover', file);
      try {
        const res = await fetch('/api/v1/games/' + currentContextGameId + '/cover', {
          method: 'POST',
          body: formData
        });
        if (res.ok) {
          loadTimestamp = Date.now();
          render();
        } else {
          alert('上传封面失败');
        }
      } catch (err) {
        alert('上传异常: ' + err);
      }
    }

    async function menuFetchOnlineCover() {
      closeContextMenu();
      if (!currentContextGameId) return;
      const game = games.find(g => g.id === currentContextGameId);
      if (!game) return;
      if (!confirm('将通过 PS Store 官方源 + 镜像源抓取 "' + (game.title || game.title_id) + '" 的封面，可能耗时 3-8 秒。\n继续？')) return;
      try {
        const r = await fetch('/api/v1/games/' + currentContextGameId + '/cover/fetch', { method: 'POST' });
        const data = await r.json().catch(() => ({}));
        if (r.ok && data.status === 'ok') {
          loadTimestamp = Date.now();
          render();
        } else {
          alert('在线抓取失败: ' + (data.error || ('HTTP ' + r.status)) + '\n请确认服务器可访问 store.playstation.com');
        }
      } catch (err) {
        alert('请求异常: ' + err);
      }
    }

    async function menuDeleteCover() {
      closeContextMenu();
      if (!currentContextGameId) return;
      try {
        const res = await fetch('/api/v1/games/' + currentContextGameId + '/cover', { method: 'DELETE' });
        if (res.ok) {
          loadTimestamp = Date.now();
          render();
        } else {
          alert('删除封面失败');
        }
      } catch (err) {
        alert('操作异常: ' + err);
      }
    }

    async function menuDeleteGame() {
      closeContextMenu();
      if (!currentContextGameId) return;
      if (!confirm('确认删除此游戏记录吗？')) return;
      try {
        const res = await fetch('/api/v1/games/' + currentContextGameId, { method: 'DELETE' });
        if (res.ok) {
          loadGames();
        } else {
          alert('删除失败');
        }
      } catch (err) {
        alert('操作异常: ' + err);
      }
    }

    /* ============== 渲染 ============== */
    function render() {
      const grid = document.getElementById('grid');
      const mode = effectiveViewMode();
      grid.classList.toggle('mode-cover', mode === 'cover');
      grid.classList.toggle('mode-icon', mode === 'icon');
      grid.innerHTML = '';

      const filtered = games.filter(g => {
        const plat = (g.platform || (g.is_ps5 ? 'PS5' : 'PS4')).toUpperCase();
        const isBase = !g.category || g.category === 'game';
        return plat === currentFilter && isBase;
      });

      if (filtered.length === 0) {
        grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1; padding:80px 0;">没有可显示的游戏。点击「重新扫描」开始扫描，或到「目录设置」配置扫描目录。</div>';
        return;
      }

      filtered.forEach(g => {
        const titleID = g.title_id || 'UNKNOWN';
        const platform = (g.platform || (g.is_ps5 ? 'PS5' : 'PS4')).toUpperCase();
        const iconSrc = '/api/v1/games/' + g.id + '/icon?v=' + loadTimestamp + '&mode=' + mode;
        const version = g.version || 'v1.0.0';
        const region = g.region || (platform === 'SWITCH' ? '全区' : '港版');
        const dlcCount = g.dlc_count || 0;

        let badgeClass = 'badge badge-ps4';
        if (platform === 'PS5') badgeClass = 'badge badge-ps5';
        else if (platform === 'PS3') badgeClass = 'badge badge-ps3';
        else if (platform === 'SWITCH') badgeClass = 'badge badge-switch';

        // ---- 刮削到的元数据（可能为空）----
        const m = g.meta || null;
        const hasMeta = !!(m && (m.summary || m.name_cn || m.art_hero));
        const year = (m && m.release_date) ? String(m.release_date).slice(0, 4) : '';
        const developer = (m && m.developer) ? m.developer : '';
        const displayTitle = (m && m.name_cn) ? m.name_cn : (g.title || '');

        let actionBtn;
        if (platform === 'SWITCH') {
          actionBtn = '<a href="/download/' + g.id + '" download class="btn-download">下载到 Switch</a>';
        } else {
          actionBtn = '<button onclick="installGame(' + g.id + ')" class="btn btn-primary btn-block">传送到 PS5</button>';
        }

        let dlcBadge = '';
        let dlcCornerTag = '';
        if (dlcCount > 0) {
          // DLC 清单纯粹从这两个入口进入：卡片右上角徽标、封面右下角角标。
          // 点击封面本身始终打开游戏详情（简介），不再拦截。
          const dlcOpen = 'event.stopPropagation(); showDLCModal(\'' + titleID + '\', \'' + (g.title || '').replace(/'/g, "\\'") + '\')';
          dlcBadge = '<button onclick="' + dlcOpen + '" class="dlc-badge" title="查看 DLC 清单">DLC (' + dlcCount + ')</button>';
          dlcCornerTag = '<span class="dlc-corner" onclick="' + dlcOpen + '" title="查看 DLC 清单">含 ' + dlcCount + ' DLC</span>';
        }

        const card = document.createElement('div');
        card.className = 'ps-card';
        const safeTitle = (g.title || '').replace(/"/g, '&quot;');
        const safeDisplay = displayTitle.replace(/"/g, '&quot;');
        card.dataset.title = g.title || 'GAME';
        card.dataset.platform = platform;
        const isSwitch = (platform === 'SWITCH');

        // 元数据摘要行：年份 · 开发商 · 评分
        let metaLine = '';
        if (year || developer || (m && m.rating > 0)) {
          const parts = [];
          if (year) parts.push('<span class="meta-year">' + year + '</span>');
          if (developer) parts.push('<span class="meta-dev" title="' + developer.replace(/"/g, '&quot;') + '">' + developer + '</span>');
          if (m && m.rating > 0) parts.push('<span class="meta-rating">★ ' + m.rating.toFixed(1) + '</span>');
          metaLine = '<div class="card-metainfo">' + parts.join('<span class="sep">·</span>') + '</div>';
        }

        const metaBadge = hasMeta ? '<span class="meta-dot" title="已刮削元数据">✦</span>' : '';

        card.innerHTML =
          '<div class="icon-container' + (isSwitch ? ' is-switch' : '') + '"' +
          (isSwitch ? ' style="--card-bg:url(&quot;' + iconSrc + '&quot;)"' : '') +
          ' onclick="openGameDetail(' + g.id + ')" oncontextmenu="showCardContextMenu(event, ' + g.id + ')">' +
          '  <img class="cover-img" src="' + iconSrc + '" alt="' + safeTitle + '" loading="lazy">' +
          dlcCornerTag +
          '</div>' +
          '<div class="card-body">' +
          '  <div>' +
          '    <div class="card-meta">' +
          '      <div class="card-meta-left">' +
          '        <span class="' + badgeClass + '">' + platform + '</span>' +
          '        <span class="title-id" title="' + titleID + '">' + titleID + '</span>' +
          metaBadge +
          '      </div>' +
          dlcBadge +
          '    </div>' +
          '    <h3 class="card-title" title="' + safeTitle + '" onclick="event.stopPropagation(); openGameDetail(' + g.id + ')">' + safeDisplay + '</h3>' +
          metaLine +
          '    <div class="card-info">' +
          '      <span class="size">' + (g.file_size / (1024*1024*1024)).toFixed(2) + ' GB</span>' +
          '      <span class="sep">·</span>' +
          '      <span class="region">' + region + '</span>' +
          '      <span class="sep">·</span>' +
          '      <span class="version">' + version + '</span>' +
          '    </div>' +
          '  </div>' +
          '  <div id="progress-wrap-' + g.id + '" class="card-progress hidden"></div>' +
          '  <div class="card-action">' + actionBtn + '</div>' +
          '</div>';
        grid.appendChild(card);
      });
      renderProgressOnly();
    }

    /* ============== DLC Modal ============== */
    async function showDLCModal(titleId, gameTitle) {
      const modal = document.getElementById('dlcModal');
      const titleIdEl = document.getElementById('dlcModalTitleId');
      const gameTitleEl = document.getElementById('dlcModalGameTitle');
      const listEl = document.getElementById('dlcModalList');
      const summaryEl = document.getElementById('dlcModalSummary');

      titleIdEl.textContent = titleId;
      gameTitleEl.textContent = gameTitle || 'DLC 追加内容';
      listEl.innerHTML = '<div class="empty-state">正在查询 DLC 数据...</div>';
      modal.classList.remove('hidden');

      try {
        const res = await fetch('/api/v1/games/dlcs?title_id=' + encodeURIComponent(titleId));
        const json = await res.json();
        const dlcs = json.data || [];
        if (dlcs.length === 0) {
          listEl.innerHTML = '<div class="empty-state">该游戏暂无关联的 DLC 文件</div>';
          summaryEl.textContent = '共 0 个追加内容';
          return;
        }
        summaryEl.textContent = '共 ' + dlcs.length + ' 个追加内容';
        listEl.innerHTML = dlcs.map(d => {
          const name = d.display_name || d.title || d.content_id || '';
          const safeName = String(name).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;');
          const rawId = String(d.content_id || d.file_path || '');
          const safeRaw = rawId.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;');
          return '<div class="dlc-item">' +
          '  <div class="info">' +
          '    <div class="name" title="' + safeRaw + '">' + safeName + '</div>' +
          '    <div class="meta">' +
          '      <span>' + (d.file_size / (1024*1024)).toFixed(1) + ' MB</span>' +
          '      <span class="sep">·</span>' +
          '      <span class="region">' + (d.region || '港版') + '</span>' +
          '      <span class="sep">·</span>' +
          '      <span>' + (d.version || 'v1.00') + '</span>' +
          '    </div>' +
          '  </div>' +
          '  <span class="tag">DLC</span>' +
          '</div>';
        }).join('');
      } catch (err) {
        listEl.innerHTML = '<div class="empty-state" style="color:var(--text-red);">获取失败: ' + err.message + '</div>';
      }
    }

    function closeDLCModal() {
      document.getElementById('dlcModal').classList.add('hidden');
    }

    /* ============== 设置弹窗 ============== */
    async function openSettingsModal() {
      try {
        const res = await fetch('/api/v1/settings');
        const json = await res.json();
        document.getElementById('modalPs5Dir').value = json.ps5_dir || '';
        document.getElementById('modalPs4Dir').value = json.ps4_dir || '';
        document.getElementById('modalSwitchDir').value = json.switch_dir || '';
        const customContainer = document.getElementById('customDirsContainer');
        customContainer.innerHTML = '';
        if (json.custom_dirs && json.custom_dirs.length > 0) {
          json.custom_dirs.forEach(d => addCustomDirInput(d));
        }
        document.getElementById('settingsModal').classList.remove('hidden');
      } catch (e) {
        alert('读取配置失败: ' + e);
      }
    }

    function addCustomDirInput(value) {
      value = value || '';
      const container = document.getElementById('customDirsContainer');
      const idx = Date.now() + Math.floor(Math.random() * 1000);
      const inputId = 'customDir_' + idx;
      const row = document.createElement('div');
      row.id = 'row_' + inputId;
      row.className = 'row';
      row.innerHTML =
        '<input type="text" id="' + inputId + '" value="' + value.replace(/"/g, '&quot;') + '" placeholder="自定义游戏路径 (如 /volume1/games)" class="custom-dir-input" style="flex:1; background:var(--bg-input-dim); border:1px solid var(--border-soft); border-radius:10px; padding:8px 12px; font-size:12px; color:var(--text); font-family:ui-monospace,monospace;">' +
        '<button type="button" onclick="openFolderPicker(\'' + inputId + '\')" class="btn btn-secondary">选择路径</button>' +
        '<button type="button" onclick="document.getElementById(\'row_' + inputId + '\').remove()" class="btn btn-danger">删除</button>';
      container.appendChild(row);
    }

    function closeSettingsModal() {
      document.getElementById('settingsModal').classList.add('hidden');
    }

    async function saveSettingsModal() {
      const customInputs = document.querySelectorAll('.custom-dir-input');
      const customDirs = [];
      customInputs.forEach(inp => {
        const v = inp.value.trim();
        if (v) customDirs.push(v);
      });
      const data = {
        ps5_dir: document.getElementById('modalPs5Dir').value.trim(),
        ps4_dir: document.getElementById('modalPs4Dir').value.trim(),
        switch_dir: document.getElementById('modalSwitchDir').value.trim(),
        custom_dirs: customDirs
      };
      const res = await fetch('/api/v1/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data)
      });
      if (res.ok) {
        closeSettingsModal();
        setTimeout(loadGames, 1000);
      } else {
        alert('保存设置失败');
      }
    }

    /* ============== 文件夹选择器 ============== */
    function openFolderPicker(targetInputId) {
      targetInputIdForPicker = targetInputId;
      const initialPath = document.getElementById(targetInputId).value.trim();
      document.getElementById('folderPickerModal').classList.remove('hidden');
      loadPickerPath(initialPath);
    }

    function closeFolderPicker() {
      document.getElementById('folderPickerModal').classList.add('hidden');
    }

    async function loadPickerPath(path) {
      const listEl = document.getElementById('pickerList');
      const pathEl = document.getElementById('pickerCurrentPath');
      listEl.innerHTML = '<div class="empty-state">正在加载目录...</div>';
      try {
        const res = await fetch('/api/v1/browse?path=' + encodeURIComponent(path || ''));
        if (!res.ok) throw new Error('路径无法读取');
        const data = await res.json();
        pickerCurrent = data.current;
        pickerParent = data.parent;
        pathEl.textContent = pickerCurrent;
        listEl.innerHTML = '';
        if (!data.dirs || data.dirs.length === 0) {
          listEl.innerHTML = '<div class="empty-state">当前目录下无子文件夹</div>';
          return;
        }
        data.dirs.forEach(d => {
          const btn = document.createElement('button');
          btn.type = 'button';
          btn.className = 'folder-item';
          const safeName = d.name.replace(/&/g, '&amp;').replace(/</g, '&lt;');
          btn.innerHTML = '<span>📁</span><span class="name">' + safeName + '</span>';
          btn.onclick = () => loadPickerPath(d.path);
          listEl.appendChild(btn);
        });
      } catch (err) {
        listEl.innerHTML = '<div class="empty-state" style="color:var(--text-red);">读取失败: ' + err.message + '</div>';
      }
    }

    function pickerGoParent() {
      if (pickerParent && pickerParent !== pickerCurrent) loadPickerPath(pickerParent);
    }

    function pickerConfirm() {
      if (targetInputIdForPicker) {
        const target = document.getElementById(targetInputIdForPicker);
        if (target) target.value = pickerCurrent;
      }
      closeFolderPicker();
    }

    /* ============== 触发扫描 ============== */
    async function triggerScan() {
      if (scanInFlight) {
        alert('扫描进行中，请稍候...');
        return;
      }
      scanInFlight = true;
      try {
        await fetch('/api/v1/scan', { method: 'POST' });
        setTimeout(() => {
          loadGames();
          scanInFlight = false;
        }, 1500);
      } catch (e) {
        scanInFlight = false;
        alert('扫描请求失败: ' + e);
      }
    }

    async function installGame(id) {
      const ps5Ip = document.getElementById('ps5IpInput').value.trim();
      if (!ps5Ip) return alert('请先输入或从列表中选择有效的 PS5 IP！');

      let res;
      try {
        res = await fetch('/api/v1/install', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ game_id: id, ps5_ip: ps5Ip, port: 9090 })
        });
      } catch (e) {
        return alert('请求异常: ' + e);
      }
      if (!res.ok) {
        const msg = await res.text();
        return alert('安装失败: ' + msg);
      }
      const resData = await res.json();
      if (!resData.task_id) {
        return alert('安装任务创建失败');
      }
      // 打开进度弹窗并开始轮询
      openInstallModal(resData.task_id);
    }

    /* ============== 安装进度弹窗 ============== */
    function openInstallModal(taskId) {
      const modal = document.getElementById('installModal');
      document.getElementById('installTaskId').value = taskId;
      document.getElementById('installTitle').textContent = '正在安装…';
      document.getElementById('installList').innerHTML = '<div class="empty-state">正在下发安装任务…</div>';
      document.getElementById('installSummary').textContent = '';
      document.getElementById('installCloseBtn').disabled = true;
      modal.classList.remove('hidden');
      pollInstall(taskId);
    }

    function closeInstallModal() {
      if (installPollTimer) { clearTimeout(installPollTimer); installPollTimer = null; }
      document.getElementById('installModal').classList.add('hidden');
    }

    const STATE_LABEL = { pending: '排队中', running: '安装中', success: '成功', failed: '失败' };
    const STATE_ICON  = { pending: '○', running: '◐', success: '✓', failed: '✕' };

    async function pollInstall(taskId) {
      let task;
      try {
        const r = await fetch('/api/v1/install/status?id=' + encodeURIComponent(taskId));
        if (!r.ok) throw new Error('HTTP ' + r.status);
        task = (await r.json()).data;
      } catch (e) {
        document.getElementById('installList').innerHTML =
          '<div class="empty-state" style="color:var(--text-red);">查询任务失败: ' + e + '</div>';
        document.getElementById('installCloseBtn').disabled = false;
        return;
      }

      // 渲染每一项
      const itemsHtml = (task.items || []).map(it => {
        const st = it.state || 'pending';
        const kindLabel = it.kind === 'base' ? '本体' : 'DLC';
        const kindCls = it.kind === 'base' ? 'kind-base' : 'kind-dlc';
        const errLine = it.error ? '<div class="inst-error">' + String(it.error).replace(/&/g,'&amp;').replace(/</g,'&lt;') + '</div>' : '';
        return '<div class="inst-item state-' + st + '">' +
          '<span class="inst-state state-' + st + '">' + (STATE_ICON[st] || '○') + '</span>' +
          '<div class="inst-info">' +
          '  <div class="inst-name" title="' + String(it.title).replace(/"/g,'&quot;') + '">' + String(it.title).replace(/&/g,'&amp;').replace(/</g,'&lt;') + '</div>' +
          errLine +
          '</div>' +
          '<span class="inst-kind ' + kindCls + '">' + kindLabel + '</span>' +
          '<span class="inst-status state-' + st + '">' + (STATE_LABEL[st] || st) + '</span>' +
          '</div>';
      }).join('');
      document.getElementById('installList').innerHTML = itemsHtml || '<div class="empty-state">无安装项</div>';

      const doneTotal = (task.done || 0) + (task.failed || 0);
      const total = task.total || 0;
      const running = (task.state === 'pending' || task.state === 'running');

      document.getElementById('installTitle').textContent =
        running ? ('正在安装 ' + doneTotal + '/' + total + '…') : '安装完成';

      const summaryEl = document.getElementById('installSummary');
      const closeBtn = document.getElementById('installCloseBtn');
      if (running) {
        summaryEl.innerHTML = '<span class="inst-progress">进度 ' + doneTotal + ' / ' + total + '</span>';
        closeBtn.disabled = true;
        installPollTimer = setTimeout(() => pollInstall(taskId), 1000);
      } else {
        const okAll = task.state === 'success';
        summaryEl.innerHTML = '<span class="' + (okAll ? 'inst-ok' : 'inst-fail') + '">' +
          (task.message || (okAll ? '安装成功' : '安装失败')) + '</span>';
        closeBtn.disabled = false;
        // 安装结束后刷新一次卡片列表（进度条可能已变化）
        loadGames();
      }
    }

    /* ============== 右键菜单：详情 / 刮削 ============== */
    function menuShowDetail() {
      closeContextMenu();
      if (currentContextGameId) openGameDetail(currentContextGameId);
    }

    async function menuScrapeGame() {
      closeContextMenu();
      if (!currentContextGameId) return;
      const game = games.find(g => g.id === currentContextGameId);
      const name = game ? (game.title || game.title_id) : ('#' + currentContextGameId);
      if (!confirm('将重新刮削「' + name + '」的元数据（简介 / 发售日 / 厂商 / 高清素材）。\n继续？')) return;
      await scrapeGame(currentContextGameId);
    }

    async function scrapeGame(gameId) {
      try {
        const r = await fetch('/api/v1/games/' + gameId + '/scrape', { method: 'POST' });
        const data = await r.json().catch(() => ({}));
        if (data.status === 'ok') {
          loadTimestamp = Date.now();
          await loadGames();
          const detailOpen = !document.getElementById('gameModal').classList.contains('hidden');
          if (detailOpen) openGameDetail(gameId, true);
        } else if (data.status === 'not_found') {
          alert('未找到匹配的元数据。\n可尝试：\n· 在文件名中加入 (igdb-游戏ID) 标签强制匹配\n· 在游戏目录放置 gamelist.xml 提供中文简介\n· 检查 IGDB 凭证是否已配置');
        } else {
          alert('刮削失败: ' + (data.error || ('HTTP ' + r.status)));
        }
      } catch (e) {
        alert('请求异常: ' + e);
      }
    }

    /* ============== 游戏详情模态框 ============== */
    function esc(s) {
      return String(s == null ? '' : s)
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
    }

    async function openGameDetail(gameId, forceReload) {
      const game = games.find(g => g.id === gameId);
      if (!game) return;

      const modal = document.getElementById('gameModal');
      const m = game.meta || {};

      // 平台与标题
      const platform = (game.platform || (game.is_ps5 ? 'PS5' : 'PS4')).toUpperCase();
      document.getElementById('detailBadges').innerHTML =
        '<span class="badge badge-' + platform.toLowerCase() + '">' + esc(platform) + '</span>' +
        '<span class="title-id">' + esc(game.title_id || '') + '</span>';

      const cn = m.name_cn || '';
      const official = game.title || '';
      document.getElementById('detailNameCN').textContent = cn || official || '未知游戏';
      const officialEl = document.getElementById('detailNameOfficial');
      officialEl.textContent = (cn && official && cn !== official) ? official : '';
      officialEl.style.display = officialEl.textContent ? '' : 'none';

      // 事实行
      const facts = [];
      if (m.release_date) facts.push('<span class="fact"><b>发售</b>' + esc(m.release_date) + '</span>');
      if (m.developer) facts.push('<span class="fact"><b>开发</b>' + esc(m.developer) + '</span>');
      if (m.publisher) facts.push('<span class="fact"><b>发行</b>' + esc(m.publisher) + '</span>');
      if (m.rating > 0) facts.push('<span class="fact"><b>评分</b>★ ' + m.rating.toFixed(1) + '</span>');
      facts.push('<span class="fact"><b>容量</b>' + (game.file_size / (1024*1024*1024)).toFixed(2) + ' GB</span>');
      if (m.igdb_id > 0) facts.push('<span class="fact"><b>IGDB</b>#' + m.igdb_id + '</span>');
      document.getElementById('detailFacts').innerHTML = facts.join('');

      // 类型标签
      if (m.genres && m.genres.length) {
        document.getElementById('detailFacts').innerHTML +=
          '<div class="detail-genres">' + m.genres.map(x => '<span class="genre-chip">' + esc(x) + '</span>').join('') + '</div>';
      }

      // 封面与 Hero
      const coverEl = document.getElementById('detailCover');
      coverEl.src = '/api/v1/games/' + gameId + '/icon?v=' + Date.now() + '&mode=cover';
      coverEl.onerror = () => { coverEl.style.visibility = 'hidden'; };

      const heroEl = document.getElementById('detailHero');
      const heroURL = m.art_hero || m.art_banner;
      if (heroURL) {
        heroEl.style.setProperty('--hero-img', 'url("/api/v1/games/' + gameId + '/art?kind=' + (m.art_hero ? 'hero' : 'banner') + '")');
        heroEl.classList.add('has-hero');
      } else {
        heroEl.classList.remove('has-hero');
        heroEl.style.removeProperty('--hero-img');
      }

      // 操作按钮
      let actions = '';
      if (platform === 'SWITCH') {
        actions += '<a href="/download/' + gameId + '" download class="btn btn-primary">下载到 Switch</a>';
      } else {
        actions += '<button onclick="installGame(' + gameId + ')" class="btn btn-primary">传送到 PS5</button>';
      }
      actions += '<button onclick="scrapeGame(' + gameId + ')" class="btn btn-secondary">重新刮削</button>';
      document.getElementById('detailActions').innerHTML = actions;

      // 简介
      const sumEl = document.getElementById('detailSummary');
      if (m.summary) {
        sumEl.textContent = m.summary;
        sumEl.classList.remove('is-empty');
      } else {
        sumEl.textContent = '暂无简介。可点击「重新刮削」重试（免 Key 的 Bangumi 中文源已默认启用），或在游戏目录放置 gamelist.xml 提供中文简介。';
        sumEl.classList.add('is-empty');
      }

      // 截图
      const shotsWrap = document.getElementById('detailShotsWrap');
      const shotsEl = document.getElementById('detailShots');
      if (m.art_screenshots && m.art_screenshots.length) {
        shotsEl.innerHTML = m.art_screenshots.map((_, i) =>
          '<img class="detail-shot" loading="lazy" src="/api/v1/games/' + gameId + '/art?kind=screenshot&i=' + i + '" alt="截图">'
        ).join('');
        shotsWrap.style.display = '';
      } else {
        shotsWrap.style.display = 'none';
      }

      modal.classList.remove('hidden');
      if (forceReload) {
        // 重新拉一次最新元数据，避免展示旧值
        try {
          const r = await fetch('/api/v1/games/' + gameId + '/metadata');
          const data = await r.json();
          if (data && data.data && game) game.meta = data.data;
        } catch (e) {}
      }
    }

    function closeGameModal() {
      document.getElementById('gameModal').classList.add('hidden');
    }

    /* ============== 元数据刮削模态框 ============== */
    let metaPollTimer = null;

    async function openMetaModal() {
      document.getElementById('metaModal').classList.remove('hidden');
      await loadMetaSettings();
      await pollMetaStatus();
    }

    function closeMetaModal() {
      if (metaPollTimer) { clearTimeout(metaPollTimer); metaPollTimer = null; }
      document.getElementById('metaModal').classList.add('hidden');
    }

    async function loadMetaSettings() {
      try {
        const r = await fetch('/api/v1/metadata/settings');
        const s = await r.json();
        document.getElementById('metaEnabled').checked = !!s.enabled;
        document.getElementById('metaBangumi').checked = s.bangumi_enabled !== false;
        document.getElementById('metaHasheous').checked = !!s.hasheous_enabled;
        document.getElementById('metaPlaymatch').checked = !!s.playmatch_enabled;
        document.getElementById('metaTitleDB').checked = s.titledb_enabled !== false;
        document.getElementById('metaIGDBId').value = s.igdb_client_id || '';
        document.getElementById('metaIGDBSecret').value = '';
        document.getElementById('metaIGDBSecret').placeholder =
          s.igdb_configured ? ('已配置（' + (s.igdb_secret_mask || '****') + '），留空表示不修改') : 'IGDB Client Secret';
        document.getElementById('metaSGDBKey').value = '';
        document.getElementById('metaSGDBKey').placeholder =
          s.sgdb_configured ? ('已配置（' + (s.sgdb_key_mask || '****') + '），留空表示不修改') : 'SteamGridDB API Key';
        document.getElementById('metaLanguage').value = (s.language || []).join(',');
        document.getElementById('metaRegion').value = (s.region || []).join(',');
      } catch (e) {
        console.error('读取元数据设置失败', e);
      }
    }

    async function saveMetaSettings() {
      const payload = {
        enabled: document.getElementById('metaEnabled').checked,
        bangumi_enabled: document.getElementById('metaBangumi').checked,
        hasheous_enabled: document.getElementById('metaHasheous').checked,
        playmatch_enabled: document.getElementById('metaPlaymatch').checked,
        titledb_enabled: document.getElementById('metaTitleDB').checked,
        igdb_client_id: document.getElementById('metaIGDBId').value.trim(),
        igdb_client_secret: document.getElementById('metaIGDBSecret').value.trim(),
        sgdb_api_key: document.getElementById('metaSGDBKey').value.trim(),
        language: document.getElementById('metaLanguage').value.split(',').map(s => s.trim()).filter(Boolean),
        region: document.getElementById('metaRegion').value.split(',').map(s => s.trim()).filter(Boolean)
      };
      try {
        const r = await fetch('/api/v1/metadata/settings', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        if (!r.ok) return alert('保存失败: HTTP ' + r.status);
        await loadMetaSettings();
        await pollMetaStatus();
        alert('已保存，设置立即生效。');
      } catch (e) {
        alert('请求异常: ' + e);
      }
    }

    async function startScrape(force) {
      if (force && !confirm('将重新刮削全部游戏的元数据，可能耗时较久。\n继续？')) return;
      try {
        const r = await fetch('/api/v1/metadata/scrape', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ platforms: [], force: force })
        });
        const data = await r.json().catch(() => ({}));
        if (data.status === 'already_running') {
          alert('已有刮削任务在运行中。');
        }
        await pollMetaStatus();
      } catch (e) {
        alert('请求异常: ' + e);
      }
    }

    // 免 Key 数据源（无需任何凭证即可工作）
    const FREE_SOURCES = { gamelist: 1, titledb: 1, hasheous: 1, playmatch: 1 };

    function renderMetaStatus(data) {
      const box = document.getElementById('metaStatusBox');
      const p = data.pipeline;
      const st = data.stats || {};
      if (p) {
        const srcLine = (s) => {
          const free = !!FREE_SOURCES[s.id];
          return '<div class="meta-src-line">' +
            '<span class="' + (s.enabled ? 'dot-on' : 'dot-off') + '"></span>' +
            esc(s.name) +
            '<span class="src-tag' + (free ? ' free' : '') + '">' + (free ? '免 Key' : '需 Key') + '</span>' +
            '</div>';
        };

        const noKey = !p.igdb_configured && !p.sgdb_configured;
        const modeLine = noKey
          ? '<div class="meta-mode ok">当前为<b>免 Key 模式</b>：中文名与中文简介由 gamelist.xml / Bangumi / Switch TitleDB 提供，PS3 哈希匹配由 Hasheous / Playmatch 提供</div>'
          : '<div class="meta-mode">已配置增强数据源：' +
            (p.igdb_configured ? '<span class="chip-ok">IGDB ✓</span>' : '') +
            (p.sgdb_configured ? '<span class="chip-ok">SteamGridDB ✓</span>' : '') +
            (p.igdb_configured ? '' : '<span class="chip-off">IGDB 未配置</span>') +
            (p.sgdb_configured ? '' : '<span class="chip-off">SteamGridDB 未配置</span>') +
            '</div>';

        box.innerHTML =
          modeLine +
          '<div class="meta-stats">' +
          '  <span>共 ' + (st.total || 0) + ' 个本体</span>' +
          '  <span>已刮削 ' + (st.scraped || 0) + '</span>' +
          '  <span>有简介 ' + (st.with_summary || 0) + '</span>' +
          '  <span>有素材 ' + (st.with_artwork || 0) + '</span>' +
          '</div>' +
          '<div class="meta-src-list">' + (p.sources || []).map(srcLine).join('') + '</div>';
      }

      const progEl = document.getElementById('metaProgress');
      const prog = data.progress;
      if (prog && prog.running) {
        progEl.classList.remove('hidden');
        const pct = prog.total > 0 ? Math.round((prog.done + prog.failed) / prog.total * 100) : 0;
        progEl.innerHTML =
          '<div class="progress-bar"><div class="progress-fill" style="width:' + pct + '%"></div></div>' +
          '<div class="progress-info"><span class="pct">' + pct + '% (' + (prog.done + prog.failed) + '/' + prog.total + ')</span>' +
          '<span>命中 ' + prog.matched + ' · 失败 ' + prog.failed + '</span></div>' +
          '<div class="meta-current">' + esc(prog.current || '') + '</div>';
      } else {
        progEl.classList.add('hidden');
      }
      return !!(prog && prog.running);
    }

    async function pollMetaStatus() {
      if (metaPollTimer) { clearTimeout(metaPollTimer); metaPollTimer = null; }
      let running = false;
      try {
        const r = await fetch('/api/v1/metadata/status');
        const data = await r.json();
        running = renderMetaStatus(data);
        if (!running) await loadGames();
      } catch (e) {
        console.error('读取刮削状态失败', e);
      }
      if (running) {
        metaPollTimer = setTimeout(pollMetaStatus, 1500);
      }
    }

    /* ============== 启动 ============== */
    document.getElementById('searchInput').addEventListener('input', (e) => loadGames(e.target.value));
    const qs = new URLSearchParams(location.search);
    // 平台过滤：?platform=PS5|PS4|SWITCH
    const urlPlat = (qs.get('platform') || '').toUpperCase();
    if (['PS5', 'PS4', 'PS3', 'SWITCH'].includes(urlPlat)) currentFilter = urlPlat;
    // 显示样式：?view=icon|cover 优先，其次 localStorage
    const urlView = qs.get('view');
    if (urlView === 'icon' || urlView === 'cover') viewMode = urlView;
    setFilter(currentFilter);
    setViewMode(viewMode);
    loadGames();
    scanDevices();
    setInterval(updateProgress, 1000);
    setInterval(() => {
      const searchVal = document.getElementById('searchInput').value;
      if (!searchVal && !scanInFlight) loadGames('');
    }, 5000);

    // 深链：?game=<id> 直接打开详情；?meta=1 打开刮削面板；?q=<关键字> 预填搜索
    const urlQuery = qs.get('q');
    if (urlQuery) {
      document.getElementById('searchInput').value = urlQuery;
      loadGames(urlQuery);
    }
    const urlGame = parseInt(qs.get('game') || '', 10);
    if (urlGame > 0) {
      const tryOpen = (tries) => {
        if (games.some(g => g.id === urlGame) || tries <= 0) { openGameDetail(urlGame); return; }
        setTimeout(() => tryOpen(tries - 1), 300);
      };
      setTimeout(() => tryOpen(10), 500);
    }
    if (qs.get('meta') === '1') {
      setTimeout(openMetaModal, 700);
    }
  </script>
</body>
</html>`
