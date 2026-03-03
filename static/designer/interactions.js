import { CANVAS_W, CANVAS_H, HANDLE_SIZE, MIN_SIZE, SNAP_ANGLE, DEFAULT_SIZE, getArrowScaleHandle, pointToSegDist, renderFrame } from '/static/render.js';
import { simEnabled, applySimState, renderSimPanel, removeFromSim } from '/static/simulator.js';

export function initInteractions(state, deps) {
  const {
    cssToCanvas, hitShape, hitChild, hitHandle, hitHeaderText, hitArrowHandle, hitLabelHandle, getLabelBounds,
    createShape, cloneShape, saveScreen, saveLayoutQuiet,
    openHeaderTextEditor, openStatusTextEditor, openLabelEditor,
    openProcessModal, openPressModal, openBufferModal, openMultiBufferModal, openFinalModal,
    isAnyModalOpen, isEditing,
  } = deps;

  const canvas = state.canvas;
  const ctx = state.ctx;
  const SCALE = state.SCALE;

  // Dragging state
  let dragging = null;
  let draggingChild = null;
  let resizing = null;
  let resizingLabel = null;
  let draggingArrow = null;
  let draggingHeaderText = null;

  const getSelected = () => state.selectedId ? state.shapes.find(s => s.id === state.selectedId) : null;

  // --- Drawing ---
  function draw() {
    applySimState(state.shapes);

    // Force all children visible on non-simulated process/press shapes
    for (const s of state.shapes) {
      if ((s.type === 'process' || s.type === 'press') && !simEnabled.has(s.id)) {
        if (s.config && s.config.children) {
          for (const child of s.config.children) {
            if (child.visible === false) child.visible = true;
          }
        }
      }
    }

    renderFrame(ctx, state.shapes);

    const sel = state.shapes.find(sh => sh.id === state.selectedId);
    if (sel) {
      const hs = HANDLE_SIZE;
      if (sel.type === 'arrow') {
        const c = sel.config;
        ctx.fillStyle = '#333';
        ctx.fillRect(c.x1 - hs / 2, c.y1 - hs / 2, hs, hs);
        ctx.fillRect(c.x2 - hs / 2, c.y2 - hs / 2, hs, hs);
        const sc = getArrowScaleHandle(c);
        ctx.save();
        ctx.translate(sc.x, sc.y);
        ctx.rotate(Math.PI / 4);
        ctx.fillStyle = '#4a90d9';
        ctx.fillRect(-hs / 2, -hs / 2, hs, hs);
        ctx.restore();
      } else if (sel.type === 'status') {
        ctx.strokeStyle = '#fff';
        ctx.lineWidth = 2;
        ctx.setLineDash([6, 4]);
        ctx.strokeRect(sel.x, sel.y, sel.w, sel.h);
        ctx.setLineDash([]);
        ctx.fillStyle = '#fff';
        const corners = [
          [sel.x, sel.y], [sel.x + sel.w, sel.y],
          [sel.x, sel.y + sel.h], [sel.x + sel.w, sel.y + sel.h],
        ];
        for (const [hx, hy] of corners) {
          ctx.fillRect(hx - hs / 2, hy - hs / 2, hs, hs);
        }
      } else if (sel.type === 'final') {
        ctx.strokeStyle = '#333';
        ctx.lineWidth = 2;
        ctx.setLineDash([6, 4]);
        ctx.strokeRect(sel.x, sel.y, sel.w, sel.h);
        ctx.setLineDash([]);
      } else {
        ctx.strokeStyle = '#333';
        ctx.lineWidth = 2;
        ctx.setLineDash([6, 4]);
        ctx.strokeRect(sel.x, sel.y, sel.w, sel.h);
        ctx.setLineDash([]);
        ctx.fillStyle = '#333';
        const corners = [
          [sel.x, sel.y], [sel.x + sel.w, sel.y],
          [sel.x, sel.y + sel.h], [sel.x + sel.w, sel.y + sel.h],
        ];
        for (const [hx, hy] of corners) {
          ctx.fillRect(hx - hs / 2, hy - hs / 2, hs, hs);
        }
        if ((sel.type === 'process' || sel.type === 'press') && sel.config.children) {
          for (const child of sel.config.children) {
            if (child.type !== 'label') continue;
            const b = getLabelBounds(ctx, sel, child);
            const midY = (b.top + b.bottom) / 2;
            ctx.fillStyle = '#333';
            ctx.fillRect(b.left - hs / 2, midY - hs / 2, hs, hs);
            ctx.fillRect(b.right - hs / 2, midY - hs / 2, hs, hs);
            ctx.strokeStyle = '#999';
            ctx.lineWidth = 1;
            ctx.setLineDash([4, 3]);
            ctx.strokeRect(b.left, b.top, b.right - b.left, b.bottom - b.top);
            ctx.setLineDash([]);
          }
        }
      }
    }

    requestAnimationFrame(draw);
  }

  // --- Palette drag & drop ---
  document.querySelectorAll('.palette-item').forEach(el => {
    el.addEventListener('dragstart', e => {
      e.dataTransfer.setData('application/x-type', el.dataset.type);
      if (el.dataset.src) e.dataTransfer.setData('application/x-src', el.dataset.src);
      e.dataTransfer.effectAllowed = 'copy';
    });
  });

  canvas.addEventListener('dragover', e => {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'copy';
  });

  canvas.addEventListener('drop', e => {
    e.preventDefault();
    const type = e.dataTransfer.getData('application/x-type');
    if (!DEFAULT_SIZE[type]) return;
    const src = e.dataTransfer.getData('application/x-src') || null;
    const { x, y } = cssToCanvas(canvas, SCALE, e.clientX, e.clientY);
    let s;
    if (type === 'header') {
      s = createShape('header', 0, 0);
    } else if (type === 'final') {
      s = createShape('final', 0, 0);
    } else if (type === 'status') {
      s = createShape('status', 0, 0);
    } else if (type === 'arrow') {
      s = createShape('arrow', x, y);
    } else {
      s = createShape(type, x - DEFAULT_SIZE[type].w / 2, y - DEFAULT_SIZE[type].h / 2, { src });
    }
    state.selectedId = s.id;
    renderSimPanel(getSelected);
  });

  // --- Canvas mouse interactions ---
  canvas.addEventListener('mousedown', e => {
    const { x, y } = cssToCanvas(canvas, SCALE, e.clientX, e.clientY);

    const headerTextHit = hitHeaderText(state.shapes, ctx, x, y);
    if (headerTextHit) {
      const c = headerTextHit.shape.config;
      const tx = c.x + (c.textX || 0.5) * c.w;
      const ty = c.y + (c.textY || 0.5) * c.h;
      state.selectedId = headerTextHit.shape.id;
      draggingHeaderText = { shapeId: headerTextHit.shape.id, offsetX: x - tx, offsetY: y - ty };
      return;
    }

    const arrowHit = hitArrowHandle(state.shapes, state.selectedId, x, y);
    if (arrowHit) {
      if (arrowHit.part === 'scale') {
        draggingArrow = { shapeId: arrowHit.shape.id, mode: 'scale' };
      } else {
        draggingArrow = { shapeId: arrowHit.shape.id, mode: arrowHit.part };
      }
      return;
    }

    const labelHit = hitLabelHandle(state.shapes, state.selectedId, ctx, x, y);
    if (labelHit) {
      resizingLabel = {
        shapeId: labelHit.shape.id,
        childId: labelHit.child.id,
        side: labelHit.side,
        startX: x,
        origScale: labelHit.child.scale || 1,
        origHalfWidth: (getLabelBounds(ctx, labelHit.shape, labelHit.child).right - getLabelBounds(ctx, labelHit.shape, labelHit.child).left) / 2,
      };
      return;
    }

    const handle = hitHandle(state.shapes, state.selectedId, x, y);
    if (handle) {
      const s = state.shapes.find(sh => sh.id === state.selectedId);
      resizing = {
        shapeId: s.id,
        handle,
        startX: x,
        startY: y,
        orig: { x: s.x, y: s.y, w: s.w, h: s.h },
      };
      return;
    }

    const childHit = hitChild(state.shapes, x, y);
    if (childHit) {
      const { shape: parentShape, child } = childHit;
      state.selectedId = parentShape.id;
      const ctrX = parentShape.config.x + child.relX * parentShape.config.h;
      const ctrY = parentShape.config.y + child.relY * parentShape.config.h;
      draggingChild = {
        shapeId: parentShape.id,
        childId: child.id,
        offsetX: x - ctrX,
        offsetY: y - ctrY,
      };
      return;
    }

    const hit = hitShape(state.shapes, x, y);
    if (hit) {
      if (hit.type === 'final') {
        state.selectedId = hit.id;
      } else if (hit.type === 'arrow') {
        if (e.altKey) {
          const clone = cloneShape(hit);
          state.selectedId = clone.id;
          draggingArrow = { shapeId: clone.id, mode: 'body', offX1: x - clone.config.x1, offY1: y - clone.config.y1, offX2: x - clone.config.x2, offY2: y - clone.config.y2 };
        } else {
          state.selectedId = hit.id;
          state.shapes = state.shapes.filter(s => s.id !== hit.id);
          state.shapes.push(hit);
          draggingArrow = { shapeId: hit.id, mode: 'body', offX1: x - hit.config.x1, offY1: y - hit.config.y1, offX2: x - hit.config.x2, offY2: y - hit.config.y2 };
        }
      } else if (e.altKey) {
        const clone = cloneShape(hit);
        state.selectedId = clone.id;
        dragging = { shapeId: clone.id, offsetX: x - clone.x, offsetY: y - clone.y };
      } else {
        state.selectedId = hit.id;
        state.shapes = state.shapes.filter(s => s.id !== hit.id);
        state.shapes.push(hit);
        dragging = { shapeId: hit.id, offsetX: x - hit.x, offsetY: y - hit.y };
      }
    } else {
      state.selectedId = null;
    }
    renderSimPanel(getSelected);
  });

  window.addEventListener('mousemove', e => {
    const { x, y } = cssToCanvas(canvas, SCALE, e.clientX, e.clientY);

    if (draggingHeaderText) {
      const s = state.shapes.find(sh => sh.id === draggingHeaderText.shapeId);
      if (s) {
        const c = s.config;
        c.textX = (x - draggingHeaderText.offsetX - c.x) / c.w;
      }
      return;
    }

    if (draggingArrow) {
      const s = state.shapes.find(sh => sh.id === draggingArrow.shapeId);
      if (s) {
        const c = s.config;
        if (draggingArrow.mode === 'head') {
          const dx = x - c.x1, dy = y - c.y1;
          const dist = Math.hypot(dx, dy);
          const angle = Math.round(Math.atan2(dy, dx) / SNAP_ANGLE) * SNAP_ANGLE;
          c.x2 = c.x1 + Math.cos(angle) * dist;
          c.y2 = c.y1 + Math.sin(angle) * dist;
        } else if (draggingArrow.mode === 'tail') {
          const dx = x - c.x2, dy = y - c.y2;
          const dist = Math.hypot(dx, dy);
          const angle = Math.round(Math.atan2(dy, dx) / SNAP_ANGLE) * SNAP_ANGLE;
          c.x1 = c.x2 + Math.cos(angle) * dist;
          c.y1 = c.y2 + Math.sin(angle) * dist;
        } else if (draggingArrow.mode === 'body') {
          c.x1 = x - draggingArrow.offX1;
          c.y1 = y - draggingArrow.offY1;
          c.x2 = x - draggingArrow.offX2;
          c.y2 = y - draggingArrow.offY2;
        } else if (draggingArrow.mode === 'scale') {
          const dist = pointToSegDist(x, y, c.x1, c.y1, c.x2, c.y2);
          c.thickness = Math.max(2, Math.min(40, dist));
        }
      }
      return;
    }

    if (resizingLabel) {
      const s = state.shapes.find(sh => sh.id === resizingLabel.shapeId);
      if (s && s.config.children) {
        const child = s.config.children.find(c => c.id === resizingLabel.childId);
        if (child) {
          const dx = x - resizingLabel.startX;
          const delta = resizingLabel.side === 'right' ? dx : -dx;
          const newHalf = Math.max(10, resizingLabel.origHalfWidth + delta);
          child.scale = Math.max(0.3, resizingLabel.origScale * (newHalf / resizingLabel.origHalfWidth));
        }
      }
      return;
    }

    if (draggingChild) {
      const s = state.shapes.find(sh => sh.id === draggingChild.shapeId);
      if (s && s.config.children) {
        const child = s.config.children.find(c => c.id === draggingChild.childId);
        if (child) {
          child.relX = (x - draggingChild.offsetX - s.config.x) / s.config.h;
          child.relY = (y - draggingChild.offsetY - s.config.y) / s.config.h;
        }
      }
      return;
    }

    if (dragging) {
      const s = state.shapes.find(sh => sh.id === dragging.shapeId);
      if (s) {
        s.x = x - dragging.offsetX;
        s.y = y - dragging.offsetY;
      }
      return;
    }

    if (resizing) {
      const s = state.shapes.find(sh => sh.id === resizing.shapeId);
      if (!s) return;
      const dx = x - resizing.startX;
      const dy = y - resizing.startY;
      const o = resizing.orig;
      const h = resizing.handle;

      let nx = o.x, ny = o.y, nw = o.w, nh = o.h;

      if (h.includes('e')) { nw = Math.max(MIN_SIZE, o.w + dx); }
      if (h.includes('w')) { nw = Math.max(MIN_SIZE, o.w - dx); nx = o.x + o.w - nw; }
      if (h.includes('s')) { nh = Math.max(MIN_SIZE, o.h + dy); }
      if (h.includes('n')) { nh = Math.max(MIN_SIZE, o.h - dy); ny = o.y + o.h - nh; }

      if (s.type === 'press') {
        nw = Math.max(nw, nh * 5 / 4);
        nh = nw * 4 / 5;
        if (h.includes('w')) nx = o.x + o.w - nw;
        if (h.includes('n')) ny = o.y + o.h - nh;
      }

      if (s.type === 'buffer') {
        const size = Math.max(nw, nh);
        if (h.includes('w')) nx = o.x + o.w - size;
        if (h.includes('n')) ny = o.y + o.h - size;
        nw = size;
        nh = size;
      }

      if (s.type === 'multibuffer') {
        nw = Math.max(nw, nh * 5 / 2);
        nh = nw * 2 / 5;
        if (h.includes('w')) nx = o.x + o.w - nw;
        if (h.includes('n')) ny = o.y + o.h - nh;
      }

      s.x = nx; s.y = ny; s.w = nw; s.h = nh;
    }
  });

  window.addEventListener('mouseup', () => {
    dragging = null;
    draggingChild = null;
    resizing = null;
    resizingLabel = null;
    draggingArrow = null;
    draggingHeaderText = null;
  });

  // --- Cursor management ---
  canvas.addEventListener('mousemove', e => {
    const { x, y } = cssToCanvas(canvas, SCALE, e.clientX, e.clientY);
    if (dragging || draggingChild || draggingArrow || draggingHeaderText) { canvas.style.cursor = 'grabbing'; return; }
    if (resizing || resizingLabel) { return; }

    if (hitHeaderText(state.shapes, ctx, x, y)) { canvas.style.cursor = 'grab'; return; }

    const ah = hitArrowHandle(state.shapes, state.selectedId, x, y);
    if (ah) { canvas.style.cursor = ah.part === 'scale' ? 'ns-resize' : 'crosshair'; return; }

    const lh = hitLabelHandle(state.shapes, state.selectedId, ctx, x, y);
    if (lh) { canvas.style.cursor = 'ew-resize'; return; }

    const handleHit = hitHandle(state.shapes, state.selectedId, x, y);
    if (handleHit) {
      const cursors = { nw: 'nwse-resize', se: 'nwse-resize', ne: 'nesw-resize', sw: 'nesw-resize' };
      canvas.style.cursor = cursors[handleHit];
      return;
    }
    if (hitChild(state.shapes, x, y)) { canvas.style.cursor = 'grab'; return; }
    const shapeHit = hitShape(state.shapes, x, y);
    canvas.style.cursor = shapeHit ? 'grab' : 'default';
  });

  // --- Double-click dispatch ---
  canvas.addEventListener('dblclick', e => {
    const { x, y } = cssToCanvas(canvas, SCALE, e.clientX, e.clientY);
    const htHit = hitHeaderText(state.shapes, ctx, x, y);
    if (htHit) {
      openHeaderTextEditor(htHit.shape);
      e.preventDefault();
      return;
    }
    const childHit = hitChild(state.shapes, x, y);
    if (childHit && childHit.child.type === 'label') {
      openLabelEditor(childHit.shape, childHit.child);
      e.preventDefault();
      return;
    }
    const hit = hitShape(state.shapes, x, y);
    if (hit && hit.type === 'process') {
      openProcessModal(hit);
      e.preventDefault();
    } else if (hit && hit.type === 'press') {
      openPressModal(hit);
      e.preventDefault();
    } else if (hit && hit.type === 'buffer') {
      openBufferModal(hit);
      e.preventDefault();
    } else if (hit && hit.type === 'multibuffer') {
      openMultiBufferModal(hit);
      e.preventDefault();
    } else if (hit && hit.type === 'arrow') {
      hit.config.showHead = hit.config.showHead === false ? true : false;
      e.preventDefault();
    } else if (hit && hit.type === 'final') {
      openFinalModal(hit);
      e.preventDefault();
    } else if (hit && hit.type === 'status') {
      openStatusTextEditor(hit);
      e.preventDefault();
    }
  });

  // --- Delete selected shape ---
  window.addEventListener('keydown', e => {
    if (isEditing()) return;
    if (isAnyModalOpen()) return;
    if (e.key === 'Delete' || e.key === 'Backspace') {
      if (state.selectedId != null) {
        const delId = state.selectedId;
        removeFromSim(delId);
        state.shapes = state.shapes.filter(s => s.id !== delId);
        state.selectedId = null;
        renderSimPanel(getSelected);
        e.preventDefault();
      }
    }
  });

  // --- Ctrl+S / Cmd+S to save ---
  window.addEventListener('keydown', e => {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
      e.preventDefault();
      saveScreen();
    }
  });

  // Save button
  document.getElementById('save-btn').addEventListener('click', saveScreen);

  // Start draw loop
  draw();

  return { draw, getSelected };
}
