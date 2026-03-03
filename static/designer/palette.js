import { drawRectangle, drawBuffer, drawMultiBuffer, drawArrow, drawHeader, drawFinal } from '/static/render.js';

export function drawPalettePreviews() {
  // Process
  const pc = document.getElementById('palette-process');
  const pctx = pc.getContext('2d');
  const miniProcessCfg = {
    x: 18, y: 4, w: 50, h: 38,
    backgroundColor: '#00FF00',
    borderColor: '#000000',
    borderWidth: 2,
    textColor: '#000000',
    glow: '#00D4FF',
    text: '99',
    children: [
      { id: 'p1', type: 'diamond',   relX: -0.22, relY: -0.05, color: '#e53935' },
      { id: 'p2', type: 'gear',      relX: -0.22, relY: 0.225, color: '#e53935' },
      { id: 'p3', type: 'stopwatch', relX: -0.22, relY: 0.5, color: '#e53935' },
      { id: 'p4', type: 'x',         relX: -0.22, relY: 0.775, color: '#e53935' },
      { id: 'p6', type: 'rabbit',    relX: -0.22, relY: 1.05, color: '#e53935' },
      { id: 'p5', type: 'label',     relX: 0.67,  relY: 1.18,  color: '#000000', text: 'OP', scale: 0.7 },
    ],
  };
  pctx.clearRect(0, 0, pc.width, pc.height);
  drawRectangle(pctx, miniProcessCfg);

  // Press
  const prC = document.getElementById('palette-press');
  const prCtx = prC.getContext('2d');
  const miniPressCfg = {
    x: 12, y: 16, w: 38, h: 30,
    backgroundColor: '#00FF00',
    borderColor: '#000000',
    borderWidth: 2,
    textColor: '#000000',
    glow: '#FF6A00',
    text: '999',
    children: [
      { id: 'p1', type: 'pm',        relX: 0.025, relY: 1.25, color: '#e53935', iconScale: 1.0 },
      { id: 'p2', type: 'wrench',    relX: 0.325, relY: 1.25, color: '#e53935', iconScale: 1.0 },
      { id: 'p3', type: 'gear',      relX: 0.625, relY: 1.25, color: '#e53935', iconScale: 1.0 },
      { id: 'p4', type: 'stopwatch', relX: 0.925, relY: 1.25, color: '#e53935', iconScale: 1.0 },
      { id: 'p5', type: 'x',         relX: 1.225, relY: 1.25, color: '#e53935', iconScale: 1.0 },
      { id: 'p6', type: 'feeder',    relX: 0.625, relY: -0.70, backgroundColor: '#00FF00', flashing: false },
    ],
  };
  prCtx.clearRect(0, 0, prC.width, prC.height);
  drawRectangle(prCtx, miniPressCfg);

  // Buffer
  const bc = document.getElementById('palette-buffer');
  const bctx = bc.getContext('2d');
  drawBuffer(bctx, { x: 2, y: 2, w: 36, h: 36, backgroundColor: '#ffffff' });

  // Multi Buffer
  const mc = document.getElementById('palette-multibuffer');
  const mctx = mc.getContext('2d');
  drawMultiBuffer(mctx, { x: 2, y: 2, w: 46, h: 26, backgroundColor: '#ffffff' });

  // Arrow
  const ac = document.getElementById('palette-arrow');
  const actx = ac.getContext('2d');
  drawArrow(actx, { x1: 4, y1: 12, x2: 46, y2: 12, thickness: 3 });

  // Header
  const hc = document.getElementById('palette-header');
  const hctx = hc.getContext('2d');
  drawHeader(hctx, { x: 0, y: 0, w: hc.width, h: hc.height, text: 'HDR', textX: 0.5, textY: 0.5 });

  // Final
  const fc = document.getElementById('palette-final');
  const fctx = fc.getContext('2d');
  drawFinal(fctx, { x: 0, y: 0, w: fc.width, h: fc.height, plan: '', actual: '', uptime: '', performance: '' });
}
