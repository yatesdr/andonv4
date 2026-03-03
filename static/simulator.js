// Simulator module for the designer — simulates PLC state on process/press shapes.
import { DEFAULT_MAPPINGS, evaluateMapping, applyMappingResult } from '/static/render.js';

export const simEnabled = new Set();
export const simData = new Map();
export const simOriginalConfig = new Map();

let visualMappings = DEFAULT_MAPPINGS;
fetch('/api/visual-mappings').then(r => r.json()).then(m => { if (m) visualMappings = m; }).catch(() => {});

function parseIntFlexible(val) {
  const s = String(val).trim();
  if (!s) return 0;
  if (s.startsWith('0x') || s.startsWith('0X')) return parseInt(s, 16) || 0;
  return parseInt(s, 10) || 0;
}

function getShapeLabel(shape) {
  if (shape.config && shape.config.children) {
    for (const c of shape.config.children) {
      if (c.type === 'label' && c.text) return c.text;
    }
  }
  return shape.type + ' ' + shape.id.slice(0, 6);
}

// Renders the sim panel for the currently selected shape.
// getSelected: () => shape | null
export function renderSimPanel(getSelected) {
  const simPanel = document.getElementById('sim-panel');
  const simToggle = document.getElementById('sim-toggle');
  const simShapeLabel = document.getElementById('sim-shape-label');
  const simBitsGrid = document.getElementById('sim-bits-grid');
  const simValuesRow = document.getElementById('sim-values-row');

  const sel = getSelected();
  if (!sel || (sel.type !== 'process' && sel.type !== 'press')) {
    simToggle.checked = false;
    simToggle.disabled = true;
    simShapeLabel.textContent = '';
    simBitsGrid.innerHTML = '<span class="sim-no-selection">Select a process or press to simulate</span>';
    simValuesRow.innerHTML = '';
    return;
  }
  simToggle.disabled = false;
  simShapeLabel.textContent = getShapeLabel(sel);

  const isOn = simEnabled.has(sel.id);
  simToggle.checked = isOn;

  const sd = isOn ? (simData.get(sel.id) || { state: 0, count: 0, buffer: 0, coilPct: 0, computedState: 0 }) : { state: 0, count: 0, buffer: 0, coilPct: 0, computedState: 0 };
  const typeMapping = visualMappings[sel.type];
  const bitLabels = typeMapping ? typeMapping.bit_labels || {} : {};

  // Partition bit labels by field prefix (state vs computedState)
  const fieldBits = {};
  for (const key of Object.keys(bitLabels)) {
    const dot = key.indexOf('.');
    if (dot < 0) continue;
    const field = key.substring(0, dot);
    const n = parseInt(key.substring(dot + 1), 10);
    if (isNaN(n)) continue;
    if (!fieldBits[field]) fieldBits[field] = -1;
    if (n > fieldBits[field]) fieldBits[field] = n;
  }

  const dis = isOn ? '' : ' disabled';

  // Build bit checkboxes for each field
  let bitsHtml = '';
  for (const [field, maxBit] of Object.entries(fieldBits)) {
    if (maxBit < 0) continue;
    bitsHtml += '<div class="sim-field-label">' + field + '</div>';
    for (let i = 0; i <= maxBit; i++) {
      const ref = field + '.' + i;
      const name = bitLabels[ref] || ref;
      const val = sd[field] || 0;
      const checked = (val & (1 << i)) ? ' checked' : '';
      bitsHtml += '<label><input type="checkbox" data-field="' + field + '" data-bit="' + i + '"' + checked + dis + '> ' + name + '</label>';
    }
  }
  simBitsGrid.innerHTML = bitsHtml;

  // Build value inputs
  let valHtml = '<label>Count <input type="text" id="sim-count" value="' + (sd.count || 0) + '"' + dis + '></label>';
  if (sel.type === 'process') {
    valHtml += '<label>Buffer <input type="text" id="sim-buffer" value="' + (sd.buffer || 0) + '"' + dis + '></label>';
  }
  if (sel.type === 'press') {
    valHtml += '<label>Coil % <input type="text" id="sim-coil" value="' + (sd.coilPct || 0) + '"' + dis + '></label>';
  }
  simValuesRow.innerHTML = valHtml;

  if (!isOn) return;

  // Wire bit checkbox handlers
  simBitsGrid.querySelectorAll('input[data-bit]').forEach(cb => {
    cb.addEventListener('change', () => {
      const field = cb.dataset.field;
      const bit = parseInt(cb.dataset.bit);
      const sd = simData.get(sel.id);
      if (!sd) return;
      if (sd[field] == null) sd[field] = 0;
      if (cb.checked) {
        sd[field] = sd[field] | (1 << bit);
      } else {
        sd[field] = sd[field] & ~(1 << bit);
      }
    });
  });

  // Wire value input handlers
  const countInp = document.getElementById('sim-count');
  if (countInp) countInp.addEventListener('input', () => {
    const sd = simData.get(sel.id);
    if (sd) sd.count = parseIntFlexible(countInp.value);
  });
  const bufInp = document.getElementById('sim-buffer');
  if (bufInp) bufInp.addEventListener('input', () => {
    const sd = simData.get(sel.id);
    if (sd) sd.buffer = parseIntFlexible(bufInp.value);
  });
  const coilInp = document.getElementById('sim-coil');
  if (coilInp) coilInp.addEventListener('input', () => {
    const sd = simData.get(sel.id);
    if (sd) sd.coilPct = parseIntFlexible(coilInp.value);
  });
}

// Toggles sim on/off for the selected shape. Call renderSimPanel after.
export function toggleSim(getSelected) {
  const simToggle = document.getElementById('sim-toggle');
  const sel = getSelected();
  if (!sel || (sel.type !== 'process' && sel.type !== 'press')) return;

  if (simToggle.checked) {
    simOriginalConfig.set(sel.id, JSON.parse(JSON.stringify(sel.config)));
    simEnabled.add(sel.id);
    if (!simData.has(sel.id)) {
      simData.set(sel.id, { state: 0, count: 0, buffer: 0, coilPct: 0, computedState: 0 });
    }
  } else {
    simEnabled.delete(sel.id);
    const orig = simOriginalConfig.get(sel.id);
    if (orig) {
      Object.assign(sel.config, orig);
      if (orig.children && sel.config.children) {
        sel.config.children.forEach((child, i) => {
          if (orig.children[i]) Object.assign(child, orig.children[i]);
        });
      }
      simOriginalConfig.delete(sel.id);
    }
    simData.delete(sel.id);
  }
}

// Applies sim state to all enabled shapes' configs.
export function applySimState(shapes) {
  for (const id of simEnabled) {
    const s = shapes.find(sh => sh.id === id);
    if (!s || !s.config) continue;
    const sd = simData.get(id);
    if (!sd) continue;
    const typeMapping = visualMappings[s.type];
    if (!typeMapping) continue;
    const result = evaluateMapping(typeMapping, sd);
    applyMappingResult(s.config, result, sd);
  }
}

// Removes a shape from sim tracking (e.g. on delete).
export function removeFromSim(id) {
  simEnabled.delete(id);
  simData.delete(id);
  simOriginalConfig.delete(id);
}
