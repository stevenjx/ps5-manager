/* =============================================================================
 * TV 端核心：音效、手柄输入、空间焦点导航
 * 移植自 Overdrive 的 useInterfaceSounds / FocusScope / useFocus 行为：
 *   - 焦点移动播放 focus.wav，越界播放 error.wav
 *   - 确认播放 confirm.wav，返回播放 back.wav
 *   - 方向键支持长按加速（首延迟 380ms，之后每 110ms 一步）
 * ========================================================================== */

const SOUND_FILES = {
  focus: "/static/tv/sounds/focus.wav",
  confirm: "/static/tv/sounds/confirm.wav",
  back: "/static/tv/sounds/back.wav",
  error: "/static/tv/sounds/error.wav",
  page: "/static/tv/sounds/page.wav",
  notify: "/static/tv/sounds/notify.wav",
};

/* ---------------------------------------------------------------- 音效 ---- */

const buffers = new Map();
let audioCtx = null;
let muted = false;

function ctx() {
  if (!audioCtx) {
    const AC = window.AudioContext || window.webkitAudioContext;
    if (!AC) return null;
    audioCtx = new AC();
  }
  if (audioCtx.state === "suspended") audioCtx.resume().catch(() => {});
  return audioCtx;
}

/** 预加载全部音效（首次用户手势后调用，避免自动播放策略拦截） */
export async function preloadSounds() {
  const c = ctx();
  if (!c) return;
  await Promise.all(
    Object.entries(SOUND_FILES).map(async ([name, url]) => {
      if (buffers.has(name)) return;
      try {
        const res = await fetch(url);
        const buf = await res.arrayBuffer();
        buffers.set(name, await c.decodeAudioData(buf));
      } catch {
        /* 音效缺失不影响功能 */
      }
    })
  );
}

/** 播放一个音效 */
export function playSound(name) {
  if (muted) return;
  const c = ctx();
  const buf = buffers.get(name);
  if (!c || !buf) return;
  const src = c.createBufferSource();
  src.buffer = buf;
  const gain = c.createGain();
  gain.gain.value = name === "focus" ? 0.35 : 0.55;
  src.connect(gain).connect(c.destination);
  src.start(0);
}

export function toggleMute() {
  muted = !muted;
  return muted;
}
export function isMuted() {
  return muted;
}

/* -------------------------------------------------------------- 手柄输入 ---- */

// 标准映射（Chrome / Edge 对 DualSense、Xbox 手柄均按此映射）
const BTN = {
  cross: 0,
  circle: 1,
  square: 2,
  triangle: 3,
  l1: 4,
  r1: 5,
  l2: 6,
  r2: 7,
  options: 9,
  up: 12,
  down: 13,
  left: 14,
  right: 15,
};

const KEYMAP = {
  ArrowUp: "up",
  ArrowDown: "down",
  ArrowLeft: "left",
  ArrowRight: "right",
  KeyW: "up",
  KeyS: "down",
  KeyA: "left",
  KeyD: "right",
  Enter: "cross",
  Space: "cross",
  Escape: "circle",
  Backspace: "circle",
  KeyT: "triangle",
  KeyF: "square",
  KeyQ: "l1",
  KeyE: "r1",
  KeyM: "mute",
};

const REPEAT_DELAY = 380; // 首次重复前的延迟
const REPEAT_RATE = 110; // 之后的重复间隔

/**
 * InputHub 统一手柄与键盘输入，输出语义动作：
 *   { type: "move", dir: "up"|"down"|"left"|"right" }
 *   { type: "press", action: "cross"|"circle"|"square"|"triangle"|"l1"|"r1" }
 */
export class InputHub {
  constructor(onAction) {
    this.onAction = onAction;
    this.held = new Map(); // action -> { since, last }
    this.padIndex = null;
    this.started = false;

    window.addEventListener("gamepadconnected", (e) => {
      this.padIndex = e.gamepad.index;
      this.start();
      this.onAction({ type: "connected" });
    });
    window.addEventListener("gamepaddisconnected", () => {
      this.padIndex = null;
      this.onAction({ type: "disconnected" });
    });

    window.addEventListener("keydown", (e) => this.onKey(e, true));
    window.addEventListener("keyup", (e) => this.onKey(e, false));

    // 首次交互解锁音频
    const unlock = () => {
      preloadSounds();
      this.start();
      window.removeEventListener("pointerdown", unlock);
      window.removeEventListener("keydown", unlock);
    };
    window.addEventListener("pointerdown", unlock);
    window.addEventListener("keydown", unlock);
  }

  start() {
    if (this.started) return;
    this.started = true;
    requestAnimationFrame(() => this.tick());
  }

  onKey(e, down) {
    const action = KEYMAP[e.code];
    if (!action) return;
    e.preventDefault();
    if (down) this.fire(action);
    else this.release(action);
  }

  fire(action) {
    if (this.held.has(action)) return;
    this.held.set(action, { since: performance.now(), last: performance.now() });
    this.emit(action);
  }

  release(action) {
    this.held.delete(action);
  }

  emit(action) {
    if (action === "mute") {
      this.onAction({ type: "mute" });
      return;
    }
    if (["up", "down", "left", "right"].includes(action)) {
      this.onAction({ type: "move", dir: action });
    } else {
      this.onAction({ type: "press", action });
    }
  }

  tick() {
    // 键盘长按重复
    const now = performance.now();
    for (const [action, st] of this.held) {
      if (!["up", "down", "left", "right"].includes(action)) continue;
      if (now - st.since < REPEAT_DELAY) continue;
      if (now - st.last < REPEAT_RATE) continue;
      st.last = now;
      this.onAction({ type: "move", dir: action });
    }

    // 手柄轮询
    const pads = navigator.getGamepads ? navigator.getGamepads() : [];
    for (const gp of pads) {
      if (!gp || gp.index !== this.padIndex) continue;
      this.pollPad(gp, now);
    }

    requestAnimationFrame(() => this.tick());
  }

  pollPad(gp, now) {
    const pressed = (i) => !!(gp.buttons[i] && gp.buttons[i].pressed);
    const map = [
      [BTN.up, "up"],
      [BTN.down, "down"],
      [BTN.left, "left"],
      [BTN.right, "right"],
      [BTN.cross, "cross"],
      [BTN.circle, "circle"],
      [BTN.square, "square"],
      [BTN.triangle, "triangle"],
      [BTN.l1, "l1"],
      [BTN.r1, "r1"],
    ];
    for (const [idx, action] of map) {
      const down = pressed(idx);
      const key = "pad:" + action;
      if (down && !this.held.has(key)) {
        this.held.set(key, { since: now, last: now });
        this.emit(action);
      } else if (!down && this.held.has(key)) {
        this.held.delete(key);
      }
    }

    // 左摇杆：超过阈值视为方向键（带死区与重复）
    const ax = gp.axes[0] || 0;
    const ay = gp.axes[1] || 0;
    const DEAD = 0.55;
    const stick = Math.abs(ax) > Math.abs(ay) ? (ax > DEAD ? "right" : ax < -DEAD ? "left" : null) : ay > DEAD ? "down" : ay < -DEAD ? "up" : null;
    const key = "stick";
    if (stick) {
      const st = this.held.get(key);
      if (!st) {
        this.held.set(key, { since: now, last: now, dir: stick });
        this.emit(stick);
      } else if (now - st.since > REPEAT_DELAY && now - st.last > REPEAT_RATE) {
        st.last = now;
        this.emit(stick);
      }
    } else if (this.held.has(key)) {
      this.held.delete(key);
    }
  }
}

/* ---------------------------------------------------------- 焦点导航引擎 ---- */

/**
 * FocusController 用「几何最近邻」做空间导航：
 *   1. 候选必须落在目标方向的半平面内；
 *   2. 与当前元素同 scope（data-scope）的优先；
 *   3. 与当前元素在垂直/水平方向有重叠的，惩罚更小（保证同一行/列优先）。
 */
export class FocusController {
  constructor({ onFocus, onBlocked } = {}) {
    this.current = null;
    this.onFocus = onFocus || (() => {});
    this.onBlocked = onBlocked || (() => {});
  }

  /** 收集当前可见的可聚焦元素 */
  items() {
    return Array.from(document.querySelectorAll("[data-focus]")).filter((el) => {
      if (el.disabled) return false;
      const r = el.getBoundingClientRect();
      if (r.width < 2 || r.height < 2) return false;
      const style = getComputedStyle(el);
      return style.visibility !== "hidden" && style.display !== "none" && style.opacity !== "0";
    });
  }

  focus(el, { silent = false } = {}) {
    if (!el || el === this.current) return !!el;
    if (this.current) this.current.classList.remove("is-focused");
    this.current = el;
    el.classList.add("is-focused");
    el.scrollIntoView({ block: "nearest", inline: "center", behavior: "smooth" });
    if (!silent) this.onFocus(el);
    return true;
  }

  /** 页面切换后把焦点放到第一个元素（或指定 selector） */
  reset(selector) {
    const target = selector ? document.querySelector(selector) : null;
    const el = target || this.items()[0];
    if (el) this.focus(el, { silent: true });
    return el;
  }

  move(dir) {
    const cur = this.current;
    if (!cur) {
      this.reset();
      return true;
    }
    const from = rectOf(cur);
    const cands = this.items().filter((el) => el !== cur);

    let best = null;
    let bestScore = Infinity;
    for (const el of cands) {
      const to = rectOf(el);
      const dx = centerX(to) - centerX(from);
      const dy = centerY(to) - centerY(from);
      const [primary, secondary] = axisDelta(dir, dx, dy);
      if (primary <= 1) continue; // 不在目标方向

      // 同 scope 优先（同一滑轨 / 同一网格）
      const sameScope = cur.dataset.scope && cur.dataset.scope === el.dataset.scope;
      // 轴对齐：主方向上另一轴的偏移越小越优先
      let score = primary + secondary * 3.2;
      if (secondary < Math.min(from.h, to.h) * 0.6) score -= primary * 0.45; // 同一行/列
      if (sameScope) score -= 220;

      if (score < bestScore) {
        bestScore = score;
        best = el;
      }
    }

    if (!best) {
      this.onBlocked(dir);
      return false;
    }
    this.focus(best);
    return true;
  }
}

function rectOf(el) {
  const r = el.getBoundingClientRect();
  return { x: r.left, y: r.top, w: r.width, h: r.height, cx: r.left + r.width / 2, cy: r.top + r.height / 2 };
}
const centerX = (r) => r.cx;
const centerY = (r) => r.cy;

function axisDelta(dir, dx, dy) {
  switch (dir) {
    case "left":
      return [-dx, Math.abs(dy)];
    case "right":
      return [dx, Math.abs(dy)];
    case "up":
      return [-dy, Math.abs(dx)];
    case "down":
      return [dy, Math.abs(dx)];
    default:
      return [0, 0];
  }
}
