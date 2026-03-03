import { CANVAS_W, CANVAS_H, DEFAULT_SIZE, DEFAULT_COLOR, loadImage, applyShapeProxy } from '/static/render.js';
import { simEnabled, simOriginalConfig } from '/static/simulator.js';

const uuid = () => crypto.randomUUID();

export function initShapes(state) {

  function createShape(type, x, y, opts) {
    const s = { id: uuid(), type };

    if (type === 'final') {
      s.config = (opts && opts.config) || {
        x: 0, y: CANVAS_H - DEFAULT_SIZE.final.h,
        w: CANVAS_W, h: DEFAULT_SIZE.final.h,
        plc: '',
        count_tag: '',
        plan: '999',
        actual: '999',
        uptime: '100%',
        performance: '100%',
      };
      applyShapeProxy(s);
      s.color = '#ffffff';
      state.shapes.push(s);
      return s;
    }

    if (type === 'status') {
      s.config = (opts && opts.config) || {
        x: 0, y: CANVAS_H / 4,
        w: CANVAS_W, h: CANVAS_H / 2,
        text: 'STATUS',
      };
      applyShapeProxy(s);
      s.color = '#000000';
      state.shapes.push(s);
      return s;
    }

    if (type === 'arrow') {
      s.config = (opts && opts.config) || {
        x1: x - 75, y1: y,
        x2: x + 75, y2: y,
        thickness: 6,
      };
      applyShapeProxy(s);
      state.shapes.push(s);
      return s;
    }

    if (type === 'process') {
      s.config = (opts && opts.config) || {
        x, y,
        w: DEFAULT_SIZE.process.w,
        h: DEFAULT_SIZE.process.h,
        backgroundColor: '#00FF00',
        borderColor: '#000000',
        borderWidth: 0,
        textColor: '#000000',
        flashing: false,
        glow: Math.random() < 0.5 ? '#FF6A00' : '#00D4FF',
        chip: 'red',
        text: '99',
        plc: '',
        count_tag: '',
        buffer_tag: '',
        state_tag: '',
        styles: [],
        reporting_units: [],
        children: [
          { id: uuid(), type: 'diamond',    relX: -0.22, relY: -0.05, color: '#e53935' },
          { id: uuid(), type: 'gear',       relX: -0.22, relY: 0.225, color: '#e53935' },
          { id: uuid(), type: 'stopwatch',  relX: -0.22, relY: 0.5, color: '#e53935' },
          { id: uuid(), type: 'x',          relX: -0.22, relY: 0.775, color: '#e53935' },
          { id: uuid(), type: 'rabbit',     relX: -0.22, relY: 1.05, color: '#e53935' },
          { id: uuid(), type: 'label',      relX: 0.67,  relY: 1.18, color: '#000000', text: 'OP-100', scale: 1 },
        ],
      };
      applyShapeProxy(s);
      s.color = s.config.backgroundColor;
    } else if (type === 'press') {
      s.config = (opts && opts.config) || {
        x, y,
        w: DEFAULT_SIZE.press.w,
        h: DEFAULT_SIZE.press.h,
        backgroundColor: '#00FF00',
        borderColor: '#000000',
        borderWidth: 0,
        textColor: '#000000',
        flashing: false,
        glow: Math.random() < 0.5 ? '#FF6A00' : '#00D4FF',
        chip: 'red',
        text: '999',
        plc: '',
        count_tag: '',
        job_count_tag: '',
        spm_tag: '',
        coil_tag: '',
        state_tag: '',
        style_tag: '',
        cat_id_1: '', cat_id_2: '', cat_id_3: '', cat_id_4: '', cat_id_5: '',
        styles: [],
        reporting_units: [],
        children: [
          { id: uuid(), type: 'pm',        relX: 0.025,  relY: 1.25, color: '#e53935', iconScale: 1.0 },
          { id: uuid(), type: 'wrench',    relX: 0.325,  relY: 1.25, color: '#e53935', iconScale: 1.0 },
          { id: uuid(), type: 'gear',      relX: 0.625,  relY: 1.25, color: '#e53935', iconScale: 1.0 },
          { id: uuid(), type: 'stopwatch', relX: 0.925,  relY: 1.25, color: '#e53935', iconScale: 1.0 },
          { id: uuid(), type: 'x',         relX: 1.225,  relY: 1.25, color: '#e53935', iconScale: 1.0 },
          { id: uuid(), type: 'label',     relX: 0.625,  relY: 1.55, color: '#000000', text: 'PR-100', scale: 1.3 },
          { id: uuid(), type: 'preflight', relX: -0.45,  relY: 0.5, backgroundColor: '#00FF00', flashing: false },
          { id: uuid(), type: 'feeder',    relX: 0.625,  relY: -0.70, backgroundColor: '#00FF00', flashing: false },
        ],
      };
      applyShapeProxy(s);
      s.color = s.config.backgroundColor;
    } else if (type === 'buffer') {
      s.config = (opts && opts.config) || {
        x, y,
        w: DEFAULT_SIZE.buffer.w,
        h: DEFAULT_SIZE.buffer.h,
        backgroundColor: '#ffffff',
        plc: '',
        tag: '',
      };
      applyShapeProxy(s);
      s.color = s.config.backgroundColor;
    } else if (type === 'multibuffer') {
      s.config = (opts && opts.config) || {
        x, y,
        w: DEFAULT_SIZE.multibuffer.w,
        h: DEFAULT_SIZE.multibuffer.h,
        backgroundColor: '#ffffff',
        textColor: '#000000',
        text: '999',
        plc: '',
        count_tag: '',
      };
      applyShapeProxy(s);
      s.color = s.config.backgroundColor;
    } else if (type === 'header') {
      s.config = (opts && opts.config) || {
        x: 0, y: 0,
        w: DEFAULT_SIZE.header.w,
        h: DEFAULT_SIZE.header.h,
        text: 'HEADER',
        textX: 0.5,
        textY: 0.5,
      };
      applyShapeProxy(s);
      s.color = '#001B3D';
    } else {
      s.x = x;
      s.y = y;
      s.w = DEFAULT_SIZE[type].w;
      s.h = DEFAULT_SIZE[type].h;
      s.color = DEFAULT_COLOR[type] || null;
      if (type === 'image' && opts && opts.src) {
        s.src = opts.src;
        const img = loadImage(opts.src);
        if (img.naturalWidth && img.naturalHeight) {
          const aspect = img.naturalWidth / img.naturalHeight;
          s.h = Math.round(s.w / aspect);
        }
      }
    }

    state.shapes.push(s);
    return s;
  }

  function cloneShape(original) {
    if (original.type === 'final') {
      const cfgCopy = JSON.parse(JSON.stringify(original.config));
      return createShape('final', 0, 0, { config: cfgCopy });
    }
    if (original.type === 'status') {
      const cfgCopy = JSON.parse(JSON.stringify(original.config));
      return createShape('status', 0, 0, { config: cfgCopy });
    }
    if (original.type === 'arrow') {
      const cfgCopy = JSON.parse(JSON.stringify(original.config));
      return createShape('arrow', 0, 0, { config: cfgCopy });
    }
    if (original.type === 'process') {
      const cfgCopy = JSON.parse(JSON.stringify(original.config));
      if (cfgCopy.children) {
        cfgCopy.children.forEach(c => { c.id = uuid(); });
      }
      return createShape('process', 0, 0, { config: cfgCopy });
    }
    if (original.type === 'press') {
      const cfgCopy = JSON.parse(JSON.stringify(original.config));
      if (cfgCopy.children) {
        cfgCopy.children.forEach(c => { c.id = uuid(); });
      }
      return createShape('press', 0, 0, { config: cfgCopy });
    }
    if (original.type === 'buffer') {
      const cfgCopy = JSON.parse(JSON.stringify(original.config));
      return createShape('buffer', 0, 0, { config: cfgCopy });
    }
    if (original.type === 'multibuffer') {
      const cfgCopy = JSON.parse(JSON.stringify(original.config));
      return createShape('multibuffer', 0, 0, { config: cfgCopy });
    }
    if (original.type === 'header') {
      const cfgCopy = JSON.parse(JSON.stringify(original.config));
      return createShape('header', 0, 0, { config: cfgCopy });
    }
    // Non-config shapes (image)
    const s = {
      id: uuid(),
      type: original.type,
      x: original.x, y: original.y,
      w: original.w, h: original.h,
      color: original.color,
    };
    if (original.src) s.src = original.src;
    state.shapes.push(s);
    return s;
  }

  function serializeShapes() {
    // Temporarily restore original configs for sim-enabled shapes
    const simSnaps = new Map();
    for (const id of simEnabled) {
      const orig = simOriginalConfig.get(id);
      const s = state.shapes.find(sh => sh.id === id);
      if (orig && s && s.config) {
        simSnaps.set(id, JSON.parse(JSON.stringify(s.config)));
        Object.assign(s.config, orig);
        if (orig.children && s.config.children) {
          s.config.children.forEach((child, i) => {
            if (orig.children[i]) Object.assign(child, orig.children[i]);
          });
        }
      }
    }

    const result = state.shapes.map(s => {
      if (s.type === 'image') {
        return { id: s.id, type: s.type, x: s.x, y: s.y, w: s.w, h: s.h, src: s.src };
      }
      return { id: s.id, type: s.type, config: s.config };
    });

    // Re-apply sim-mutated configs so draw loop continues
    for (const [id, mutated] of simSnaps) {
      const s = state.shapes.find(sh => sh.id === id);
      if (s && s.config) {
        Object.assign(s.config, mutated);
        if (mutated.children && s.config.children) {
          s.config.children.forEach((child, i) => {
            if (mutated.children[i]) Object.assign(child, mutated.children[i]);
          });
        }
      }
    }

    return result;
  }

  function saveLayoutQuiet() {
    if (!state.screenID) return;
    fetch('/api/screens/' + state.screenID + '/layout', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(serializeShapes()),
    });
  }

  async function saveScreen() {
    const screenNameInput = document.getElementById('screen-name');
    const saveStatus = document.getElementById('save-status');
    const name = screenNameInput.value.trim();
    if (!name) {
      screenNameInput.focus();
      return;
    }

    saveStatus.textContent = 'Saving...';

    try {
      // Create screen if new
      if (!state.screenID) {
        const resp = await fetch('/api/screens', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name }),
        });
        if (!resp.ok) throw new Error('Failed to create screen');
        const sc = await resp.json();
        state.screenID = sc.id;
      } else {
        // Update name
        await fetch('/api/screens/' + state.screenID, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name }),
        });
      }

      // Save layout
      const layout = serializeShapes();
      const resp = await fetch('/api/screens/' + state.screenID + '/layout', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(layout),
      });
      if (!resp.ok) throw new Error('Failed to save layout');

      window.location.href = '/';
    } catch (err) {
      saveStatus.textContent = 'Error: ' + err.message;
    }
  }

  return { createShape, cloneShape, serializeShapes, saveLayoutQuiet, saveScreen };
}
