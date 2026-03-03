import { HANDLE_SIZE, pointToSegDist, getArrowScaleHandle } from '/static/render.js';

export function cssToCanvas(canvas, SCALE, cssX, cssY) {
  const rect = canvas.getBoundingClientRect();
  return {
    x: (cssX - rect.left) * SCALE,
    y: (cssY - rect.top) * SCALE,
  };
}

export function shapeContains(s, cx, cy) {
  if (s.type === 'final') {
    return cx >= s.x && cx <= s.x + s.w && cy >= s.y && cy <= s.y + s.h;
  }
  if (s.type === 'status') {
    return cx >= s.x && cx <= s.x + s.w && cy >= s.y && cy <= s.y + s.h;
  }
  if (s.type === 'arrow') {
    const c = s.config;
    return pointToSegDist(cx, cy, c.x1, c.y1, c.x2, c.y2) <= Math.max(10, c.thickness * 2);
  }
  if (s.type === 'buffer') {
    const rx = s.w / 2, ry = s.h / 2;
    const dx = cx - (s.x + rx), dy = cy - (s.y + ry);
    return (dx * dx) / (rx * rx) + (dy * dy) / (ry * ry) <= 1;
  }
  return cx >= s.x && cx <= s.x + s.w && cy >= s.y && cy <= s.y + s.h;
}

export function hitShape(shapes, cx, cy) {
  for (let i = shapes.length - 1; i >= 0; i--) {
    if (shapeContains(shapes[i], cx, cy)) return shapes[i];
  }
  return null;
}

export function hitChild(shapes, cx, cy) {
  for (let i = shapes.length - 1; i >= 0; i--) {
    const s = shapes[i];
    if ((s.type !== 'process' && s.type !== 'press') || !s.config.children) continue;
    const childSize = s.config.h * 0.28;
    for (const child of s.config.children) {
      const ctrX = s.config.x + child.relX * s.config.h;
      const ctrY = s.config.y + child.relY * s.config.h;
      if (cx >= ctrX - childSize / 2 && cx <= ctrX + childSize / 2 && cy >= ctrY - childSize / 2 && cy <= ctrY + childSize / 2) {
        return { shape: s, child };
      }
    }
  }
  return null;
}

export function getLabelBounds(ctx, s, child) {
  const h = s.config.h;
  const childSize = h * 0.28;
  const scale = child.scale || 1;
  const fontSize = Math.max(12, childSize * 1.0 * scale);
  ctx.font = `900 ${fontSize}px "Arial Black", "Impact", sans-serif`;
  const text = child.text || 'OP-100';
  const metrics = ctx.measureText(text);
  const cx = s.config.x + child.relX * h;
  const cy = s.config.y + child.relY * h;
  const tw = metrics.width / 2;
  const th = fontSize / 2;
  return { left: cx - tw, right: cx + tw, top: cy - th, bottom: cy + th, cx, cy };
}

export function hitLabelHandle(shapes, selectedId, ctx, px, py) {
  const s = shapes.find(sh => sh.id === selectedId);
  if (!s || (s.type !== 'process' && s.type !== 'press') || !s.config.children) return null;
  const hs = HANDLE_SIZE;
  for (const child of s.config.children) {
    if (child.type !== 'label') continue;
    const b = getLabelBounds(ctx, s, child);
    const midY = (b.top + b.bottom) / 2;
    if (px >= b.left - hs && px <= b.left + hs && py >= midY - hs && py <= midY + hs) {
      return { shape: s, child, side: 'left' };
    }
    if (px >= b.right - hs && px <= b.right + hs && py >= midY - hs && py <= midY + hs) {
      return { shape: s, child, side: 'right' };
    }
  }
  return null;
}

export function hitHandle(shapes, selectedId, cx, cy) {
  const s = shapes.find(sh => sh.id === selectedId);
  if (!s || s.type === 'final') return null;
  const hs = HANDLE_SIZE;
  const corners = [
    { name: 'nw', hx: s.x,       hy: s.y },
    { name: 'ne', hx: s.x + s.w, hy: s.y },
    { name: 'sw', hx: s.x,       hy: s.y + s.h },
    { name: 'se', hx: s.x + s.w, hy: s.y + s.h },
  ];
  for (const c of corners) {
    if (cx >= c.hx - hs && cx <= c.hx + hs && cy >= c.hy - hs && cy <= c.hy + hs) {
      return c.name;
    }
  }
  return null;
}

export function hitHeaderText(shapes, ctx, cx, cy) {
  for (let i = shapes.length - 1; i >= 0; i--) {
    const s = shapes[i];
    if (s.type !== 'header' || !s.config.text) continue;
    const c = s.config;
    const fontSize = c.h * 0.9;
    ctx.font = `900 ${fontSize}px "Arial Black", "Impact", sans-serif`;
    const tw = ctx.measureText(c.text).width / 2;
    const th = fontSize / 2;
    const tx = c.x + (c.textX || 0.5) * c.w;
    const ty = c.y + (c.textY || 0.5) * c.h;
    if (cx >= tx - tw && cx <= tx + tw && cy >= ty - th && cy <= ty + th) {
      return { shape: s };
    }
  }
  return null;
}

export function hitArrowHandle(shapes, selectedId, cx, cy) {
  const s = shapes.find(sh => sh.id === selectedId);
  if (!s || s.type !== 'arrow') return null;
  const c = s.config;
  const hs = HANDLE_SIZE;
  if (cx >= c.x2 - hs && cx <= c.x2 + hs && cy >= c.y2 - hs && cy <= c.y2 + hs) return { shape: s, part: 'head' };
  if (cx >= c.x1 - hs && cx <= c.x1 + hs && cy >= c.y1 - hs && cy <= c.y1 + hs) return { shape: s, part: 'tail' };
  const sc = getArrowScaleHandle(c);
  if (cx >= sc.x - hs && cx <= sc.x + hs && cy >= sc.y - hs && cy <= sc.y + hs) return { shape: s, part: 'scale' };
  return null;
}
