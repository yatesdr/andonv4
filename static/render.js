// render.js — Shared rendering module for Andon Canvas
// Used by both the designer (index.html) and display (display.html)

// --- Constants ---
export const CANVAS_W = 1920;
export const CANVAS_H = 1080;
export const HANDLE_SIZE = 10;
export const MIN_SIZE = 20;
export const SNAP_ANGLE = Math.PI / 12; // 15 degrees
export const DEFAULT_SIZE = { process: { w: 160, h: 120 }, press: { w: 160, h: 128 }, buffer: { w: 46, h: 46 }, multibuffer: { w: 115, h: 46 }, image: { w: 200, h: 200 }, header: { w: 1920, h: 120 }, arrow: { w: 150, h: 0 }, final: { w: 1920, h: 120 } };
export const DEFAULT_COLOR = { process: '#4a90d9' };

// --- Flash timer (shared across all flashing shapes) ---
export let flashOn = true;
let flashTimerStarted = false;
export function startFlashTimer() {
  if (flashTimerStarted) return;
  flashTimerStarted = true;
  setInterval(() => { flashOn = !flashOn; }, 300);
}

// --- Image cache ---
export const imageCache = {};
export function loadImage(src) {
  if (imageCache[src]) return imageCache[src];
  const img = new Image();
  img.src = src;
  imageCache[src] = img;
  return img;
}

// --- Border width helper ---
function calcBorder(w, h, min, factor) {
  return Math.max(min, Math.sqrt(w * h) * factor);
}

// --- Child icon draw functions ---
export const childDrawers = {
  x(ctx, cx, cy, sz, color) {
    const r = sz * 0.25;
    ctx.strokeStyle = color;
    ctx.lineWidth = Math.max(3, sz * 0.22);
    ctx.lineCap = 'butt';
    ctx.beginPath();
    ctx.moveTo(cx - r, cy - r); ctx.lineTo(cx + r, cy + r);
    ctx.moveTo(cx + r, cy - r); ctx.lineTo(cx - r, cy + r);
    ctx.stroke();
  },
  diamond(ctx, cx, cy, sz, color) {
    const r = sz * 0.4;
    ctx.fillStyle = color;
    ctx.beginPath();
    ctx.moveTo(cx, cy - r);
    ctx.lineTo(cx + r, cy);
    ctx.lineTo(cx, cy + r);
    ctx.lineTo(cx - r, cy);
    ctx.closePath();
    ctx.fill();
  },
  gear(ctx, cx, cy, sz, color) {
    const outer = sz * 0.4, inner = sz * 0.28, teeth = 8;
    ctx.fillStyle = color;
    ctx.beginPath();
    for (let i = 0; i < teeth * 2; i++) {
      const a = (Math.PI * 2 * i) / (teeth * 2) - Math.PI / 2;
      const r = i % 2 === 0 ? outer : inner;
      const px = cx + Math.cos(a) * r, py = cy + Math.sin(a) * r;
      i === 0 ? ctx.moveTo(px, py) : ctx.lineTo(px, py);
    }
    ctx.closePath();
    ctx.fill();
    ctx.fillStyle = '#fff';
    ctx.beginPath();
    ctx.arc(cx, cy, sz * 0.1, 0, Math.PI * 2);
    ctx.fill();
  },
  label(ctx, cx, cy, sz, color, child) {
    const scale = child.scale || 1;
    const text = child.text || 'OP-100';
    const fontSize = Math.max(12, sz * 1.0 * scale);
    ctx.font = `900 ${fontSize}px "Arial Black", "Impact", sans-serif`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillStyle = color;
    ctx.fillText(text, cx, cy);
  },
  wrench(ctx, cx, cy, sz, color) {
    const r = sz * 0.4;
    ctx.fillStyle = color;
    ctx.save();
    ctx.translate(cx, cy);
    ctx.rotate(-Math.PI / 4);
    const sw = r * 0.35;
    const hw = r * 0.6;
    ctx.beginPath();
    ctx.moveTo(-sw / 2, r * 1.0);
    ctx.lineTo(sw / 2, r * 1.0);
    ctx.lineTo(sw / 2, -r * 0.05);
    ctx.lineTo(hw / 2, -r * 0.05);
    ctx.lineTo(hw / 2, -r * 0.5);
    ctx.lineTo(sw / 4, -r * 0.5);
    ctx.lineTo(sw / 4, -r * 0.25);
    ctx.lineTo(-sw / 4, -r * 0.25);
    ctx.lineTo(-sw / 4, -r * 0.5);
    ctx.lineTo(-hw / 2, -r * 0.5);
    ctx.lineTo(-hw / 2, -r * 0.05);
    ctx.lineTo(-sw / 2, -r * 0.05);
    ctx.closePath();
    ctx.fill();
    ctx.restore();
  },
  pm(ctx, cx, cy, sz, color) {
    const fontSize = sz * 0.55;
    ctx.font = `900 ${fontSize}px "Arial Black", "Impact", sans-serif`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillStyle = color;
    ctx.fillText('PM', cx, cy);
  },
  preflight(ctx, cx, cy, sz, color, child, parentCfg) {
    if (!parentCfg) return;
    const rectH = parentCfg.h * 0.8;
    const rectW = rectH * 0.4;
    const rx = cx - rectW / 2;
    const ry = cy - rectH / 2;
    ctx.fillStyle = (child.flashing && !flashOn) ? '#ffffff' : (child.backgroundColor || '#00FF00');
    ctx.fillRect(rx, ry, rectW, rectH);
    const borderW = calcBorder(rectW, rectH, 2, 0.025);
    ctx.strokeStyle = child.borderColor || '#000000';
    ctx.lineWidth = borderW;
    ctx.lineJoin = 'miter';
    ctx.strokeRect(rx, ry, rectW, rectH);
  },
  feeder(ctx, cx, cy, sz, color, child, parentCfg) {
    if (!parentCfg) return;
    const rectH = parentCfg.h * 0.5;
    const rectW = rectH * (4 / 3);
    const rx = cx - rectW / 2;
    const ry = cy - rectH / 2;
    const radius = Math.min(rectW, rectH) * 0.15;
    ctx.fillStyle = (child.flashing && !flashOn) ? '#ffffff' : (child.backgroundColor || '#00FF00');
    ctx.beginPath();
    ctx.roundRect(rx, ry, rectW, rectH, radius);
    ctx.fill();
    const borderW = calcBorder(rectW, rectH, 2, 0.025);
    ctx.strokeStyle = child.borderColor || '#000000';
    ctx.lineWidth = borderW;
    ctx.beginPath();
    ctx.roundRect(rx, ry, rectW, rectH, radius);
    ctx.stroke();
  },
  rabbit(ctx, cx, cy, sz, color) {
    const r = sz * 0.4;
    ctx.fillStyle = color;
    ctx.save();
    ctx.translate(cx, cy);
    // Facing right profile — single ear angled back
    ctx.beginPath();
    ctx.ellipse(r * 0.05, -r * 0.85, r * 0.15, r * 0.5, 0.3, 0, Math.PI * 2);
    ctx.fill();
    // Head
    ctx.beginPath();
    ctx.ellipse(r * 0.2, -r * 0.2, r * 0.35, r * 0.3, 0, 0, Math.PI * 2);
    ctx.fill();
    // Body — larger oval behind head
    ctx.beginPath();
    ctx.ellipse(-r * 0.25, r * 0.15, r * 0.5, r * 0.38, -0.15, 0, Math.PI * 2);
    ctx.fill();
    // Tail — small circle at rear
    ctx.beginPath();
    ctx.arc(-r * 0.7, r * 0.0, r * 0.14, 0, Math.PI * 2);
    ctx.fill();
    // Hind leg
    ctx.beginPath();
    ctx.ellipse(-r * 0.25, r * 0.5, r * 0.22, r * 0.18, 0.3, 0, Math.PI * 2);
    ctx.fill();
    // Front leg
    ctx.beginPath();
    ctx.ellipse(r * 0.2, r * 0.45, r * 0.12, r * 0.16, 0, 0, Math.PI * 2);
    ctx.fill();
    ctx.restore();
  },
  stopwatch(ctx, cx, cy, sz, color) {
    const r = sz * 0.35;
    ctx.strokeStyle = color;
    ctx.lineWidth = Math.max(2, sz * 0.1);
    ctx.beginPath();
    ctx.arc(cx, cy + sz * 0.05, r, 0, Math.PI * 2);
    ctx.stroke();
    ctx.lineCap = 'round';
    ctx.beginPath();
    ctx.moveTo(cx, cy + sz * 0.05 - r);
    ctx.lineTo(cx, cy + sz * 0.05 - r - sz * 0.12);
    ctx.stroke();
    ctx.beginPath();
    ctx.moveTo(cx, cy + sz * 0.05);
    const ha = -Math.PI / 3;
    ctx.lineTo(cx + Math.cos(ha) * r * 0.65, cy + sz * 0.05 + Math.sin(ha) * r * 0.65);
    ctx.stroke();
  },
};

// --- Draw functions ---

export function drawRectangle(ctx, cfg) {
  const { x, y, w, h } = cfg;
  const bg = cfg.backgroundColor || '#4a90d9';

  if (cfg.glow && !(cfg.glowFlashing && !flashOn)) {
    const gc = cfg.glow;
    const scale = Math.sqrt(1.2);
    const gw = w * scale, gh = h * scale;
    const gx = x - (gw - w) / 2, gy = y - (gh - h) / 2;
    ctx.save();
    ctx.shadowColor = gc;
    ctx.shadowBlur = calcBorder(w, h, 10, 0.1);
    ctx.shadowOffsetX = 0;
    ctx.shadowOffsetY = 0;
    ctx.fillStyle = gc;
    ctx.globalAlpha = 0.9;
    ctx.fillRect(gx, gy, gw, gh);
    ctx.globalAlpha = 0.7;
    ctx.fillRect(gx, gy, gw, gh);
    ctx.globalAlpha = 0.5;
    ctx.fillRect(gx, gy, gw, gh);
    ctx.restore();
  }

  ctx.fillStyle = (cfg.flashing && !flashOn) ? '#ffffff' : bg;
  ctx.fillRect(x, y, w, h);
  const borderW = cfg.borderWidth || calcBorder(w, h, 4, 0.04);
  ctx.strokeStyle = cfg.borderColor || '#000000';
  ctx.lineWidth = borderW;
  ctx.lineJoin = 'miter';
  ctx.strokeRect(x, y, w, h);

  if (cfg.chip) {
    const chipSide = Math.sqrt(0.05 * w * h);
    const bw = cfg.borderWidth || calcBorder(w, h, 4, 0.04);
    const pad = bw / 2 + 1;
    ctx.fillStyle = cfg.chip[0] === '#' ? cfg.chip : (cfg.chip === 'red' ? '#FF0000' : '#00FF00');
    ctx.fillRect(x + pad, y + pad, chipSide, chipSide);
  }

  if (cfg.text) {
    const fontSize = Math.max(14, Math.min(h * 0.4, w * 0.4));
    ctx.font = `bold ${fontSize}px -apple-system, BlinkMacSystemFont, sans-serif`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillStyle = cfg.textColor || '#000000';
    ctx.fillText(cfg.text, x + w / 2, y + h / 2);
  }

  if (cfg.children) {
    const baseChildSize = h * 0.28;
    for (const child of cfg.children) {
      if (child.visible === false) continue;
      const drawer = childDrawers[child.type];
      if (drawer) {
        const childSize = baseChildSize * (child.iconScale || 1);
        const absCx = x + child.relX * h;
        const absCy = y + child.relY * h;
        drawer(ctx, absCx, absCy, childSize, child.color || '#e53935', child, cfg);
      }
    }
  }
}

export function drawBuffer(ctx, cfg) {
  const { x, y, w, h } = cfg;
  const bg = cfg.backgroundColor || '#ffffff';
  const cx = x + w / 2, cy = y + h / 2;
  const rx = w / 2, ry = h / 2;
  ctx.fillStyle = bg;
  ctx.beginPath();
  ctx.ellipse(cx, cy, rx, ry, 0, 0, Math.PI * 2);
  ctx.fill();
  const borderW = calcBorder(w, h, 4, 0.04);
  ctx.strokeStyle = '#000000';
  ctx.lineWidth = borderW;
  ctx.beginPath();
  ctx.ellipse(cx, cy, rx, ry, 0, 0, Math.PI * 2);
  ctx.stroke();
}

export function drawMultiBuffer(ctx, cfg) {
  const { x, y, w, h } = cfg;
  const bg = cfg.backgroundColor || '#ffffff';
  ctx.fillStyle = bg;
  ctx.fillRect(x, y, w, h);
  const borderW = calcBorder(w, h, 4, 0.04);
  ctx.strokeStyle = '#000000';
  ctx.lineWidth = borderW;
  ctx.lineJoin = 'miter';
  ctx.strokeRect(x, y, w, h);
  if (cfg.text) {
    const fontSize = Math.max(10, h * 0.8);
    ctx.font = `bold ${fontSize}px -apple-system, BlinkMacSystemFont, sans-serif`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillStyle = cfg.textColor || '#000000';
    ctx.fillText(cfg.text, x + w / 2, y + h / 2);
  }
}

export function drawFinal(ctx, cfg) {
  const { x, y, w, h } = cfg;
  const boxW = w / 4;
  const labels = ['PLAN', 'ACTUAL', 'UPTIME', 'PERFORMANCE'];
  const values = [cfg.plan || '999', cfg.actual || '999', cfg.uptime || '100%', cfg.performance || '100%'];
  const labelH = h * 0.25;
  const borderW = calcBorder(boxW, h, 3, 0.03);

  const grad = ctx.createLinearGradient(x, y, x + w, y);
  grad.addColorStop(0, '#001B3D');
  grad.addColorStop(0.5, '#003A7A');
  grad.addColorStop(1, '#001B3D');
  ctx.fillStyle = grad;
  ctx.fillRect(x, y, w, labelH);

  for (let i = 0; i < 4; i++) {
    const bx = x + i * boxW;
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(bx, y + labelH, boxW, h - labelH);
    ctx.strokeStyle = '#000000';
    ctx.lineWidth = borderW;
    ctx.lineJoin = 'miter';
    ctx.strokeRect(bx, y, boxW, h);
    const labelFontSize = labelH * 0.85;
    ctx.font = `900 ${labelFontSize}px "Arial Black", "Impact", sans-serif`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillStyle = '#FFFFFF';
    ctx.fillText(labels[i], bx + boxW / 2, y + labelH / 2);
    const valueFontSize = (h - labelH) * 0.85;
    ctx.font = `900 ${valueFontSize}px "Arial Black", "Impact", sans-serif`;
    ctx.fillStyle = '#000000';
    ctx.fillText(values[i], bx + boxW / 2, y + labelH + (h - labelH) / 2);
  }
}

export function drawArrow(ctx, cfg) {
  const { x1, y1, x2, y2 } = cfg;
  const th = cfg.thickness || 6;
  const angle = Math.atan2(y2 - y1, x2 - x1);
  const showHead = cfg.showHead !== false;
  const headLen = th * 5;

  ctx.strokeStyle = '#000000';
  ctx.lineWidth = th;
  ctx.lineCap = 'butt';
  ctx.beginPath();
  ctx.moveTo(x1, y1);
  if (showHead) {
    const leX = x2 - Math.cos(angle) * headLen * 0.6;
    const leY = y2 - Math.sin(angle) * headLen * 0.6;
    ctx.lineTo(leX, leY);
  } else {
    ctx.lineTo(x2, y2);
  }
  ctx.stroke();

  if (showHead) {
    ctx.fillStyle = '#000000';
    ctx.beginPath();
    ctx.moveTo(x2, y2);
    ctx.lineTo(x2 - headLen * Math.cos(angle - 0.45), y2 - headLen * Math.sin(angle - 0.45));
    ctx.lineTo(x2 - headLen * Math.cos(angle + 0.45), y2 - headLen * Math.sin(angle + 0.45));
    ctx.closePath();
    ctx.fill();
  }
}

export function drawHeader(ctx, cfg) {
  const { x, y, w, h } = cfg;
  const grad = ctx.createLinearGradient(x, y, x + w, y);
  grad.addColorStop(0, '#001B3D');
  grad.addColorStop(0.5, '#003A7A');
  grad.addColorStop(1, '#001B3D');
  ctx.fillStyle = grad;
  ctx.fillRect(x, y, w, h);
  const hlGrad = ctx.createLinearGradient(x, y + h - 3, x, y + h);
  hlGrad.addColorStop(0, 'rgba(74,144,217,0.5)');
  hlGrad.addColorStop(1, 'rgba(74,144,217,0)');
  ctx.fillStyle = hlGrad;
  ctx.fillRect(x, y + h - 3, w, 3);
  if (cfg.text) {
    const fontSize = h * 0.9;
    ctx.font = `900 ${fontSize}px "Arial Black", "Impact", sans-serif`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillStyle = '#FFFFFF';
    const tx = x + (cfg.textX || 0.5) * w;
    const ty = y + (cfg.textY || 0.5) * h;
    ctx.fillText(cfg.text, tx, ty);
  }
}

// --- Helpers ---

export function pointToSegDist(px, py, x1, y1, x2, y2) {
  const dx = x2 - x1, dy = y2 - y1;
  const lenSq = dx * dx + dy * dy;
  if (lenSq === 0) return Math.hypot(px - x1, py - y1);
  let t = ((px - x1) * dx + (py - y1) * dy) / lenSq;
  t = Math.max(0, Math.min(1, t));
  return Math.hypot(px - (x1 + t * dx), py - (y1 + t * dy));
}

export function getArrowScaleHandle(cfg) {
  const midX = (cfg.x1 + cfg.x2) / 2;
  const midY = (cfg.y1 + cfg.y2) / 2;
  const angle = Math.atan2(cfg.y2 - cfg.y1, cfg.x2 - cfg.x1);
  const perp = angle - Math.PI / 2;
  const off = (cfg.thickness || 6) * 2 + 15;
  return { x: midX + Math.cos(perp) * off, y: midY + Math.sin(perp) * off };
}

// --- Shape proxy ---
// Applies Object.defineProperty proxies for x/y/w/h based on shape type.
// The shape must already have .type and .config set. Does nothing for image type.

export function applyShapeProxy(s) {
  if (s.type === 'arrow') {
    Object.defineProperty(s, 'x', { get() { return Math.min(s.config.x1, s.config.x2); }, set() {}, enumerable: true });
    Object.defineProperty(s, 'y', { get() { return Math.min(s.config.y1, s.config.y2); }, set() {}, enumerable: true });
    Object.defineProperty(s, 'w', { get() { return Math.abs(s.config.x2 - s.config.x1); }, set() {}, enumerable: true });
    Object.defineProperty(s, 'h', { get() { return Math.abs(s.config.y2 - s.config.y1) || 1; }, set() {}, enumerable: true });
    return s;
  }
  if (s.type === 'image') return s;
  for (const prop of ['x', 'y', 'w', 'h']) {
    Object.defineProperty(s, prop, {
      get() { return s.config[prop]; },
      set(v) { s.config[prop] = v; },
      enumerable: true,
    });
  }
  return s;
}

// --- Hydrate shapes from JSON ---
// Takes a plain JSON array and returns shape objects with proper defineProperty proxies

export function hydrateShapes(data) {
  return data.map(d => {
    const s = { id: d.id, type: d.type };

    if (d.type === 'image') {
      s.x = d.x; s.y = d.y; s.w = d.w; s.h = d.h;
      s.src = d.src;
      s.color = d.color || null;
      loadImage(s.src);
      return s;
    }

    s.config = d.config;
    applyShapeProxy(s);
    s.color = d.color || s.config.backgroundColor || null;
    return s;
  });
}

// --- Hit testing ---
// Returns { shape, child } or null. Skips arrow/status/image/header.
export function hitTest(shapes, cx, cy) {
  for (let i = shapes.length - 1; i >= 0; i--) {
    const s = shapes[i];
    if (s.type === 'arrow' || s.type === 'status' || s.type === 'image' || s.type === 'header') continue;
    const cfg = s.config || {};
    if (cfg.children) {
      const h = cfg.h;
      const baseChildSize = h * 0.28;
      for (const child of cfg.children) {
        if (child.type === 'label') continue;
        const childSize = baseChildSize * (child.iconScale || 1);
        const absCx = cfg.x + child.relX * h;
        const absCy = cfg.y + child.relY * h;
        let hitW, hitH;
        if (child.type === 'preflight') {
          hitH = h * 0.8; hitW = hitH * 0.4;
        } else if (child.type === 'feeder') {
          hitH = h * 0.5; hitW = hitH * (4 / 3);
        } else {
          hitW = childSize; hitH = childSize;
        }
        if (cx >= absCx - hitW / 2 && cx <= absCx + hitW / 2 &&
            cy >= absCy - hitH / 2 && cy <= absCy + hitH / 2) {
          return { shape: s, child };
        }
      }
    }
    if (cx >= s.x && cx <= s.x + s.w && cy >= s.y && cy <= s.y + s.h) {
      return { shape: s, child: null };
    }
  }
  return null;
}

// --- Main render frame (5-pass) ---
// Pure visual rendering — no selection handles (those are editor-only)

export function renderFrame(ctx, shapes) {
  ctx.clearRect(0, 0, CANVAS_W, CANVAS_H);
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, CANVAS_W, CANVAS_H);

  // Pass 1: arrows (behind everything)
  for (const s of shapes) {
    if (s.type !== 'arrow') continue;
    drawArrow(ctx, s.config);
  }

  // Pass 2: standard shapes (except arrows, status, final)
  for (const s of shapes) {
    if (s.type === 'arrow' || s.type === 'status' || s.type === 'final') continue;
    if (s.type === 'process' || s.type === 'press') {
      drawRectangle(ctx, s.config);
    } else if (s.type === 'header') {
      drawHeader(ctx, s.config);
    } else if (s.type === 'image') {
      const img = imageCache[s.src];
      if (img && img.complete) {
        ctx.drawImage(img, s.x, s.y, s.w, s.h);
      }
    } else if (s.type === 'buffer') {
      drawBuffer(ctx, s.config);
    } else if (s.type === 'multibuffer') {
      drawMultiBuffer(ctx, s.config);
    }
  }

  // Pass 3: final component
  for (const s of shapes) {
    if (s.type !== 'final') continue;
    drawFinal(ctx, s.config);
  }

}

// --- Data-driven visual mapping engine ---

export const DEFAULT_MAPPINGS = {
  process: {
    bit_labels: {
      'state.0': 'heartbeat', 'state.1': 'inAuto', 'state.2': 'inManual',
      'state.3': 'faulted', 'state.4': 'inCycle', 'state.5': 'eStop',
      'state.6': 'clearToEnter', 'state.7': 'lcBroken', 'state.8': 'cycleStart',
      'state.9': 'starved', 'state.10': 'blocked',
      'state.11': 'redRabbit', 'state.12': 'mhPartPresent', 'state.13': 'staPartPresent',
      'state.14': 'prodAndon', 'state.15': 'maintAndon',
      'state.16': 'logisticsAndon', 'state.17': 'qualityAndon',
      'state.18': 'hrAndon', 'state.19': 'emergencyAndon',
      'state.20': 'toolingAndon', 'state.21': 'engineeringAndon',
      'state.22': 'controlsAndon', 'state.23': 'itAndon',
      'state.24': 'partKicked', 'state.25': 'toolChangeActive',
      'computedState.0': 'behind', 'computedState.1': 'overcycle',
      'computedState.2': 'firstHourMet', 'computedState.3': 'firstHourComplete'
    },
    mapping: {
      backgroundColor: [
        { condition: { type: 'default' }, value: '#C0C0C0' },
        { condition: { type: 'bits', any: ['state.2'] }, value: '#FFFF00' },
        { condition: { type: 'bits', any: ['state.1'] }, value: '#00FF00' },
        { condition: { type: 'bits', all: ['computedState.1', 'state.1'] }, value: '#00FF00' },
        { condition: { type: 'bits', all: ['computedState.1', 'state.1', 'state.4'] }, value: '#FFA500' },
        { condition: { type: 'bits', any: ['state.3', 'state.5'] }, value: '#FF0000' },
        { condition: { type: 'bits', any: ['state.25'] }, value: '#8B00FF' }
      ],
      borderColor: [
        { condition: { type: 'default' }, value: '#000000' },
        { condition: { type: 'bits', any: ['computedState.0'] }, value: '#FF0000' }
      ],
      flashing: [
        { condition: { type: 'default' }, value: false },
        { condition: { type: 'bits', all: ['computedState.1', 'state.1'] }, value: true }
      ],
      glow: [
        { condition: { type: 'default' }, value: null },
        { condition: { type: 'bits', any: ['state.14', 'state.16', 'state.17', 'state.18', 'state.21', 'state.22', 'state.23'] }, value: '#00D4FF' },
        { condition: { type: 'bits', any: ['state.15', 'state.20', 'state.22'] }, value: '#FF6A00' },
        { condition: { type: 'bits', any: ['state.19'] }, value: '#FF0000' }
      ],
      glowFlashing: [
        { condition: { type: 'default' }, value: false },
        { condition: { type: 'bits', any: ['state.19'] }, value: true }
      ],
      chip: [
        { condition: { type: 'default' }, value: '#ff0000' },
        { condition: { type: 'bits', any: ['computedState.2'] }, value: '#00ff00' }
      ],
      text: [
        { condition: { type: 'default' }, value: '-' },
        { condition: { type: 'has_field', field: 'count' }, value: { copy_from: 'count', decode: 'high16' } }
      ],
      'child.diamond.visible': [
        { condition: { type: 'default' }, value: false },
        { condition: { type: 'bits', any: ['state.9'] }, value: true }
      ],
      'child.x.visible': [
        { condition: { type: 'default' }, value: false },
        { condition: { type: 'bits', any: ['state.10'] }, value: true }
      ],
      'child.gear.visible': [
        { condition: { type: 'default' }, value: false },
        { condition: { type: 'bits', any: ['state.15', 'state.3'] }, value: true }
      ],
      'child.stopwatch.visible': [
        { condition: { type: 'default' }, value: false },
        { condition: { type: 'bits', any: ['computedState.1'] }, value: true }
      ],
      'child.rabbit.visible': [
        { condition: { type: 'default' }, value: false },
        { condition: { type: 'bits', any: ['state.11'] }, value: true }
      ],
      statusMessage: [
        { condition: { type: 'default' }, value: null },
        { condition: { type: 'bits', any: ['state.14'] }, value: 'PROD' },
        { condition: { type: 'bits', any: ['state.15'] }, value: 'MAINT' },
        { condition: { type: 'bits', any: ['state.16'] }, value: 'LOGISTICS' },
        { condition: { type: 'bits', any: ['state.17'] }, value: 'QUALITY' },
        { condition: { type: 'bits', any: ['state.18'] }, value: 'HR' },
        { condition: { type: 'bits', any: ['state.19'] }, value: 'EMERGENCY' },
        { condition: { type: 'bits', any: ['state.20'] }, value: 'TOOLING' },
        { condition: { type: 'bits', any: ['state.21'] }, value: 'ENGINEERING' },
        { condition: { type: 'bits', any: ['state.22'] }, value: 'CONTROLS' },
        { condition: { type: 'bits', any: ['state.23'] }, value: 'IT CALL' }
      ]
    }
  },
  press: {
    bit_labels: {
      'state.0': 'heartbeat', 'state.1': 'inAuto', 'state.2': 'inContinuous',
      'state.3': 'dieChange', 'state.4': 'faulted', 'state.5': 'eStop',
      'state.6': 'topStop', 'state.7': 'setup1', 'state.8': 'setup2',
      'state.9': 'setup3', 'state.10': 'setup4',
      'state.11': 'coilEnd', 'state.12': 'dieProtect', 'state.13': 'lubeProtect',
      'state.14': 'strokeComplete',
      'computedState.0': 'behind', 'computedState.1': 'overcycle',
      'computedState.2': 'firstHourMet', 'computedState.3': 'firstHourComplete'
    },
    mapping: {
      backgroundColor: [
        { condition: { type: 'default' }, value: '#C0C0C0' },
        { condition: { type: 'has_field', field: 'state' }, value: '#FFFF00' },
        { condition: { type: 'bits', any: ['state.2'] }, value: '#00FF00' },
        { condition: { type: 'bits', any: ['state.1'] }, value: '#00FF00' },
        { condition: { type: 'bits', all: ['computedState.1', 'state.1'] }, value: '#00FF00' },
        { condition: { type: 'bits', any: ['state.4', 'state.5', 'state.6'] }, value: '#FF0000' },
        { condition: { type: 'bits', any: ['state.3'] }, value: '#8B00FF' }
      ],
      borderColor: [
        { condition: { type: 'default' }, value: '#000000' },
        { condition: { type: 'bits', any: ['computedState.0'] }, value: '#FF0000' }
      ],
      flashing: [
        { condition: { type: 'default' }, value: false },
        { condition: { type: 'has_field', field: 'state' }, value: true },
        { condition: { type: 'bits', any: ['state.2'] }, value: false },
        { condition: { type: 'bits', any: ['state.1'] }, value: true },
        { condition: { type: 'bits', any: ['state.4', 'state.5', 'state.6'] }, value: false },
        { condition: { type: 'bits', any: ['state.3'] }, value: false },
        { condition: { type: 'bits', all: ['computedState.1', 'state.1'] }, value: true }
      ],
      glow: [
        { condition: { type: 'default' }, value: null }
      ],
      chip: [
        { condition: { type: 'default' }, value: '#C0C0C0' },
        { condition: { type: 'has_field', field: 'state' }, value: { source: 'self', field: 'backgroundColor' } }
      ],
      text: [
        { condition: { type: 'default' }, value: '-' },
        { condition: { type: 'has_field', field: 'count' }, value: { copy_from: 'count' } }
      ],
      'child.gear.visible': [
        { condition: { type: 'default' }, value: false },
        { condition: { type: 'bits', any: ['state.7', 'state.8', 'state.9', 'state.10'] }, value: true }
      ],
      'child.x.visible': [
        { condition: { type: 'default' }, value: false },
        { condition: { type: 'bits', any: ['state.12'] }, value: true }
      ],
      'child.stopwatch.visible': [
        { condition: { type: 'default' }, value: false }
      ],
      'child.wrench.visible': [
        { condition: { type: 'default' }, value: false }
      ],
      'child.pm.visible': [
        { condition: { type: 'default' }, value: false }
      ],
      'child.feeder.backgroundColor': [
        { condition: { type: 'default' }, value: '#C0C0C0' },
        { condition: { type: 'range', field: 'coilPct', min: 30.01 }, value: '#00FF00' },
        { condition: { type: 'range', field: 'coilPct', min: 5, max: 30 }, value: '#FFA500' },
        { condition: { type: 'range', field: 'coilPct', min: 0, max: 4.99 }, value: '#FF0000' },
        { condition: { type: 'bits', any: ['state.11'] }, value: '#FF0000' }
      ],
      'child.feeder.borderColor': [
        { condition: { type: 'default' }, value: '#000000' }
      ],
      'child.preflight.backgroundColor': [
        { condition: { type: 'default' }, value: '#C0C0C0' }
      ],
      'child.preflight.borderColor': [
        { condition: { type: 'default' }, value: '#000000' }
      ],
      statusMessage: [
        { condition: { type: 'default' }, value: null },
        { condition: { type: 'bits', any: ['state.4', 'state.5', 'state.6'] }, value: 'FAULT' },
        { condition: { type: 'bits', any: ['state.3'] }, value: 'DIE CHANGE' }
      ]
    }
  }
};

export function resolveBit(ref, data) {
  if (!data || typeof ref !== 'string') return false;
  let invert = false;
  let r = ref;
  if (r[0] === '!') {
    invert = true;
    r = r.substring(1);
  }
  const dot = r.indexOf('.');
  if (dot < 0) return false;
  const field = r.substring(0, dot);
  const bit = parseInt(r.substring(dot + 1), 10);
  if (isNaN(bit)) return false;
  const val = data[field];
  if (val == null) return false;
  const set = !!(val & (1 << bit));
  return invert ? !set : set;
}

export function matchCondition(condition, data) {
  if (!condition) return false;
  switch (condition.type) {
    case 'default':
      return true;
    case 'bits':
      if (condition.all) {
        return condition.all.every(ref => resolveBit(ref, data));
      }
      if (condition.any) {
        return condition.any.some(ref => resolveBit(ref, data));
      }
      return false;
    case 'range': {
      if (!data) return false;
      const val = data[condition.field];
      if (val == null) return false;
      if (condition.min != null && val < condition.min) return false;
      if (condition.max != null && val > condition.max) return false;
      return true;
    }
    case 'has_field':
      return data != null && data[condition.field] != null;
    default:
      return false;
  }
}

export function evaluateMapping(typeMapping, data) {
  if (!typeMapping || !typeMapping.mapping) return {};
  const result = {};
  for (const [action, rules] of Object.entries(typeMapping.mapping)) {
    for (const rule of rules) {
      if (matchCondition(rule.condition, data)) {
        result[action] = rule.value;
      }
    }
  }
  return result;
}

function applyPropValue(target, prop, value, data) {
  if (value === undefined) return;
  if (value != null && typeof value === 'object') {
    // Generic copy_from: read any data field, optional decode
    // Backward compat: treat value.source (when not 'self') as copy_from
    const field = value.copy_from || (value.source !== 'self' ? value.source : null);
    if (field && data && data[field] != null) {
      const raw = data[field];
      if (value.decode === 'high16') {
        target[prop] = String((raw >> 16) & 0xFFFF);
      } else if (value.decode === 'low16') {
        target[prop] = String(raw & 0xFFFF);
      } else {
        target[prop] = String(raw);
      }
    } else if (value.source === 'self') {
      // Deferred — handled in second pass
      return;
    }
  } else {
    target[prop] = value;
  }
}

export function applyMappingResult(cfg, result, data) {
  if (!cfg) return;
  const directProps = ['backgroundColor', 'flashing', 'glow', 'glowFlashing', 'chip', 'text', 'borderColor'];
  // First pass: direct properties
  for (const prop of directProps) {
    if (result[prop] !== undefined) {
      applyPropValue(cfg, prop, result[prop], data);
    }
  }
  // Second pass: resolve self-references
  for (const prop of directProps) {
    const val = result[prop];
    if (val != null && typeof val === 'object' && val.source === 'self') {
      cfg[prop] = cfg[val.field];
    }
  }
  // Third pass: child properties (child.TYPE.PROPERTY)
  if (cfg.children) {
    for (const child of cfg.children) {
      if (child.type === 'label') continue;
      for (const [action, value] of Object.entries(result)) {
        if (!action.startsWith('child.')) continue;
        const parts = action.split('.');
        if (parts.length !== 3) continue;
        const childType = parts[1];
        const childProp = parts[2];
        if (child.type === childType) {
          applyPropValue(child, childProp, value, data);
        }
      }
    }
  }
}
