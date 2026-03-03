import { initFilterDropdown, wireModal } from '/static/modals.js';

export function initPropertyModals(state, deps) {
  const { saveLayoutQuiet, warlinkURL } = deps;

  const processModalOverlay = document.getElementById('process-modal-overlay');
  const pressModalOverlay = document.getElementById('press-modal-overlay');
  const bufferModalOverlay = document.getElementById('buffer-modal-overlay');
  const mbufModalOverlay = document.getElementById('mbuf-modal-overlay');
  const finalModalOverlay = document.getElementById('final-modal-overlay');

  let modalShapeId = null;
  let PLC_OPTIONS = [];
  let TAG_OPTIONS = [];

  // --- Warlink connection manager ---
  const warlink = {
    configured: warlinkURL !== '',
    plcs: [],
    tagsByPlc: {},
  };

  const warlinkStatusEl = document.getElementById('warlink-status');

  function setWarlinkStatus(st, label) {
    warlinkStatusEl.className = 'warlink-status ' + st;
    warlinkStatusEl.querySelector('.warlink-label').textContent = label;
  }

  function updateAllPlcDropdowns() {
    PLC_OPTIONS = warlink.plcs.map(p => p.name);
    [ddProcPLC, ddPressPLC, ddBufPLC, ddMbufPLC, ddFinalPLC].forEach(dd => dd.setOptions(PLC_OPTIONS));
  }

  async function updateTagDropdownsForPlc(plcName, ...tagDropdowns) {
    if (!plcName) { tagDropdowns.forEach(dd => dd.setOptions([])); return; }
    if (!warlink.tagsByPlc[plcName]) {
      try { await fetchTagsForPlc(plcName); } catch (_) {}
    }
    const tags = (warlink.tagsByPlc[plcName] || []).map(t => t.name);
    tagDropdowns.forEach(dd => dd.setOptions(tags));
  }

  async function fetchPlcs() {
    const resp = await fetch('/api/warlink/plcs');
    if (!resp.ok) throw new Error('Failed to fetch PLCs');
    warlink.plcs = await resp.json();
  }

  async function fetchTagsForPlc(plcName) {
    const resp = await fetch('/api/warlink/tags/' + encodeURIComponent(plcName));
    if (!resp.ok) throw new Error('Failed to fetch tags for ' + plcName);
    warlink.tagsByPlc[plcName] = await resp.json();
  }

  async function initWarlink() {
    if (!warlink.configured) {
      setWarlinkStatus('', 'No Warlink');
      return;
    }
    setWarlinkStatus('connecting', 'Connecting...');
    try {
      await fetchPlcs();
      updateAllPlcDropdowns();
      setWarlinkStatus('connected', 'Connected');
    } catch (err) {
      console.error('Warlink init failed:', err);
      setWarlinkStatus('disconnected', 'Disconnected');
    }
  }

  // --- Process dropdowns ---
  const ddProcPLC = initFilterDropdown('dd-proc-plc', PLC_OPTIONS);
  const ddProcCountTag = initFilterDropdown('dd-proc-count-tag', TAG_OPTIONS);
  const ddProcBufferTag = initFilterDropdown('dd-proc-buffer-tag', TAG_OPTIONS);
  const ddProcStateTag = initFilterDropdown('dd-proc-state-tag', TAG_OPTIONS);
  const ddProcRu = initFilterDropdown('dd-proc-ru', []);

  ddProcPLC.input.addEventListener('change', () => updateTagDropdownsForPlc(ddProcPLC.input.value, ddProcCountTag, ddProcBufferTag, ddProcStateTag));
  ddProcPLC.input.addEventListener('blur', () => updateTagDropdownsForPlc(ddProcPLC.input.value, ddProcCountTag, ddProcBufferTag, ddProcStateTag));

  // --- Press dropdowns ---
  const ddPressPLC = initFilterDropdown('dd-press-plc', PLC_OPTIONS);
  const ddPressCountTag = initFilterDropdown('dd-press-count-tag', TAG_OPTIONS);
  const ddPressJobCountTag = initFilterDropdown('dd-press-job-count-tag', TAG_OPTIONS);
  const ddPressSpmTag = initFilterDropdown('dd-press-spm-tag', TAG_OPTIONS);
  const ddPressCoilTag = initFilterDropdown('dd-press-coil-tag', TAG_OPTIONS);
  const ddPressStateTag = initFilterDropdown('dd-press-state-tag', TAG_OPTIONS);
  const ddPressStyleTag = initFilterDropdown('dd-press-style-tag', TAG_OPTIONS);
  const ddPressCatId1 = initFilterDropdown('dd-press-cat-id-1', TAG_OPTIONS);
  const ddPressCatId2 = initFilterDropdown('dd-press-cat-id-2', TAG_OPTIONS);
  const ddPressCatId3 = initFilterDropdown('dd-press-cat-id-3', TAG_OPTIONS);
  const ddPressCatId4 = initFilterDropdown('dd-press-cat-id-4', TAG_OPTIONS);
  const ddPressCatId5 = initFilterDropdown('dd-press-cat-id-5', TAG_OPTIONS);
  const ddPressRu = initFilterDropdown('dd-press-ru', []);

  const pressTagDDs = [ddPressCountTag, ddPressJobCountTag, ddPressSpmTag, ddPressCoilTag, ddPressStateTag, ddPressStyleTag, ddPressCatId1, ddPressCatId2, ddPressCatId3, ddPressCatId4, ddPressCatId5];
  ddPressPLC.input.addEventListener('change', () => updateTagDropdownsForPlc(ddPressPLC.input.value, ...pressTagDDs));
  ddPressPLC.input.addEventListener('blur', () => updateTagDropdownsForPlc(ddPressPLC.input.value, ...pressTagDDs));

  // --- Buffer dropdowns ---
  const ddBufPLC = initFilterDropdown('dd-buf-plc', PLC_OPTIONS);
  const ddBufTag = initFilterDropdown('dd-buf-tag', TAG_OPTIONS);
  ddBufPLC.input.addEventListener('change', () => updateTagDropdownsForPlc(ddBufPLC.input.value, ddBufTag));
  ddBufPLC.input.addEventListener('blur', () => updateTagDropdownsForPlc(ddBufPLC.input.value, ddBufTag));

  // --- Multi Buffer dropdowns ---
  const ddMbufPLC = initFilterDropdown('dd-mbuf-plc', PLC_OPTIONS);
  const ddMbufCountTag = initFilterDropdown('dd-mbuf-count-tag', TAG_OPTIONS);
  ddMbufPLC.input.addEventListener('change', () => updateTagDropdownsForPlc(ddMbufPLC.input.value, ddMbufCountTag));
  ddMbufPLC.input.addEventListener('blur', () => updateTagDropdownsForPlc(ddMbufPLC.input.value, ddMbufCountTag));

  // --- Final dropdowns ---
  const ddFinalPLC = initFilterDropdown('dd-final-plc', PLC_OPTIONS);
  const ddFinalCountTag = initFilterDropdown('dd-final-count-tag', TAG_OPTIONS);
  ddFinalPLC.input.addEventListener('change', () => updateTagDropdownsForPlc(ddFinalPLC.input.value, ddFinalCountTag));
  ddFinalPLC.input.addEventListener('blur', () => updateTagDropdownsForPlc(ddFinalPLC.input.value, ddFinalCountTag));

  // --- Shared modal state ---
  let modalStyles = [];
  let modalReportingUnits = [];
  let selectedBgColor = '#00FF00';
  let activeStylesListEl = null;
  let activeRuChipsEl = null;

  function renderModalStyles() {
    if (!activeStylesListEl) return;
    if (modalStyles.length === 0) {
      activeStylesListEl.innerHTML = '<div style="color:#999;font-size:12px;padding:4px 0;">No styles defined</div>';
      return;
    }
    let html = '';
    modalStyles.forEach((st, i) => {
      html += '<div style="display:flex;gap:6px;align-items:center;margin-bottom:4px;">'
        + '<input type="text" value="' + (st.name || '').replace(/"/g, '&quot;') + '" data-sidx="' + i + '" data-sfield="name" placeholder="Name" style="flex:1;padding:4px 6px;border:1px solid #ccc;border-radius:3px;font-size:13px;">'
        + '<input type="number" value="' + (st.value != null ? st.value : '') + '" data-sidx="' + i + '" data-sfield="value" placeholder="Value" style="width:70px;padding:4px 6px;border:1px solid #ccc;border-radius:3px;font-size:13px;">'
        + '<button type="button" data-sidx="' + i + '" style="background:none;border:none;color:#ccc;font-size:16px;cursor:pointer;padding:0 4px;line-height:1;" class="modal-style-rm">&times;</button>'
        + '</div>';
    });
    activeStylesListEl.innerHTML = html;
  }

  function renderRuChips() {
    if (!activeRuChipsEl) return;
    if (modalReportingUnits.length === 0) {
      activeRuChipsEl.innerHTML = '';
      return;
    }
    activeRuChipsEl.innerHTML = modalReportingUnits.map((tag, i) =>
      '<span class="chip">' + tag.replace(/</g, '&lt;')
      + '<button type="button" class="chip-rm" data-ridx="' + i + '">&times;</button></span>'
    ).join('');
  }

  // Delegated event handlers for styles lists
  document.addEventListener('input', e => {
    const inp = e.target;
    if (inp.dataset.sidx === undefined || !activeStylesListEl || !activeStylesListEl.contains(inp)) return;
    const i = +inp.dataset.sidx;
    if (inp.dataset.sfield === 'name') modalStyles[i].name = inp.value;
    if (inp.dataset.sfield === 'value') modalStyles[i].value = inp.value === '' ? null : parseInt(inp.value);
  });

  document.addEventListener('click', e => {
    // Style remove buttons
    const rmBtn = e.target.closest('.modal-style-rm');
    if (rmBtn && activeStylesListEl && activeStylesListEl.contains(rmBtn)) {
      modalStyles.splice(+rmBtn.dataset.sidx, 1);
      renderModalStyles();
      return;
    }
    // RU chip remove buttons
    const chipBtn = e.target.closest('.chip-rm');
    if (chipBtn && activeRuChipsEl && activeRuChipsEl.contains(chipBtn)) {
      modalReportingUnits.splice(+chipBtn.dataset.ridx, 1);
      renderRuChips();
    }
  });

  // Add style buttons
  document.getElementById('proc-modal-add-style').addEventListener('click', () => {
    activeStylesListEl = document.getElementById('proc-modal-styles-list');
    modalStyles.push({ name: '', value: null });
    renderModalStyles();
    const inputs = activeStylesListEl.querySelectorAll('input[data-sfield="name"]');
    if (inputs.length) inputs[inputs.length - 1].focus();
  });

  document.getElementById('press-modal-add-style').addEventListener('click', () => {
    activeStylesListEl = document.getElementById('press-modal-styles-list');
    modalStyles.push({ name: '', value: null });
    renderModalStyles();
    const inputs = activeStylesListEl.querySelectorAll('input[data-sfield="name"]');
    if (inputs.length) inputs[inputs.length - 1].focus();
  });

  // --- Reporting Units ---
  let globalReportingUnits = [];

  async function fetchReportingUnits() {
    try {
      const resp = await fetch('/api/reporting-units');
      if (resp.ok) globalReportingUnits = await resp.json();
    } catch (_) {}
    ddProcRu.setOptions(globalReportingUnits);
    ddPressRu.setOptions(globalReportingUnits);
  }

  async function addGlobalReportingUnit(name) {
    if (!globalReportingUnits.includes(name)) {
      globalReportingUnits.push(name);
      globalReportingUnits.sort();
      ddProcRu.setOptions(globalReportingUnits);
      ddPressRu.setOptions(globalReportingUnits);
      try {
        await fetch('/api/reporting-units', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(globalReportingUnits),
        });
      } catch (_) {}
    }
  }

  function addRuChip(val) {
    if (!val || modalReportingUnits.includes(val)) return;
    modalReportingUnits.push(val);
    addGlobalReportingUnit(val);
    renderRuChips();
  }

  function setupRuDropdown(dd) {
    dd.input.addEventListener('change', () => {
      const val = dd.input.value.trim();
      addRuChip(val);
      dd.input.value = '';
    });
    dd.input.addEventListener('keydown', e => {
      if (e.key === 'Enter') {
        e.preventDefault();
        const val = dd.input.value.trim();
        addRuChip(val);
        dd.input.value = '';
      }
    });
  }
  setupRuDropdown(ddProcRu);
  setupRuDropdown(ddPressRu);

  // --- Background color pickers ---
  function setupBgColorPicker(containerSel) {
    const opts = document.querySelectorAll(containerSel + ' .bg-opt');
    opts.forEach(opt => {
      opt.addEventListener('click', () => {
        selectedBgColor = opt.dataset.color;
        opts.forEach(o => o.style.borderColor = o === opt ? '#333' : 'transparent');
      });
    });
    return opts;
  }
  const procBgOpts = setupBgColorPicker('.bg-color-picker[data-modal="process"]');
  const pressBgOpts = setupBgColorPicker('.bg-color-picker[data-modal="press"]');

  // --- Process modal ---
  function openProcessModal(shape) {
    modalShapeId = shape.id;
    selectedBgColor = shape.config.backgroundColor || '#00FF00';
    procBgOpts.forEach(o => o.style.borderColor = o.dataset.color === selectedBgColor ? '#333' : 'transparent');
    ddProcPLC.input.value = shape.config.plc || '';
    updateTagDropdownsForPlc(ddProcPLC.input.value, ddProcCountTag, ddProcBufferTag, ddProcStateTag);
    ddProcCountTag.input.value = shape.config.count_tag || '';
    ddProcBufferTag.input.value = shape.config.buffer_tag || '';
    ddProcStateTag.input.value = shape.config.state_tag || '';
    activeStylesListEl = document.getElementById('proc-modal-styles-list');
    modalStyles = (shape.config.styles || []).map(s => ({ name: s.name, value: s.value }));
    renderModalStyles();
    activeRuChipsEl = document.getElementById('proc-modal-ru-chips');
    modalReportingUnits = [...(shape.config.reporting_units || [])];
    renderRuChips();
    ddProcRu.input.value = '';
    document.getElementById('dd-proc-plc').classList.remove('open');
    document.getElementById('dd-proc-count-tag').classList.remove('open');
    document.getElementById('dd-proc-buffer-tag').classList.remove('open');
    document.getElementById('dd-proc-state-tag').classList.remove('open');
    document.getElementById('dd-proc-ru').classList.remove('open');
    processModalOverlay.classList.add('open');
  }

  function closeProcessModal(save) {
    if (save && modalShapeId != null) {
      const s = state.shapes.find(sh => sh.id === modalShapeId);
      if (s && s.config) {
        s.config.backgroundColor = selectedBgColor;
        s.config.plc = ddProcPLC.input.value;
        s.config.count_tag = ddProcCountTag.input.value;
        s.config.buffer_tag = ddProcBufferTag.input.value;
        s.config.state_tag = ddProcStateTag.input.value;
        s.config.styles = modalStyles.filter(st => st.name);
        s.config.reporting_units = [...modalReportingUnits];
      }
      saveLayoutQuiet();
    }
    processModalOverlay.classList.remove('open');
    modalShapeId = null;
  }

  wireModal('process-modal-overlay', 'proc-modal-save', 'proc-modal-cancel', closeProcessModal);

  // --- Press modal ---
  function openPressModal(shape) {
    modalShapeId = shape.id;
    selectedBgColor = shape.config.backgroundColor || '#00FF00';
    pressBgOpts.forEach(o => o.style.borderColor = o.dataset.color === selectedBgColor ? '#333' : 'transparent');
    ddPressPLC.input.value = shape.config.plc || '';
    updateTagDropdownsForPlc(ddPressPLC.input.value, ...pressTagDDs);
    ddPressCountTag.input.value = shape.config.count_tag || '';
    ddPressJobCountTag.input.value = shape.config.job_count_tag || '';
    ddPressSpmTag.input.value = shape.config.spm_tag || '';
    ddPressCoilTag.input.value = shape.config.coil_tag || '';
    ddPressStateTag.input.value = shape.config.state_tag || '';
    ddPressStyleTag.input.value = shape.config.style_tag || '';
    ddPressCatId1.input.value = shape.config.cat_id_1 || '';
    ddPressCatId2.input.value = shape.config.cat_id_2 || '';
    ddPressCatId3.input.value = shape.config.cat_id_3 || '';
    ddPressCatId4.input.value = shape.config.cat_id_4 || '';
    ddPressCatId5.input.value = shape.config.cat_id_5 || '';
    activeStylesListEl = document.getElementById('press-modal-styles-list');
    modalStyles = (shape.config.styles || []).map(s => ({ name: s.name, value: s.value }));
    renderModalStyles();
    activeRuChipsEl = document.getElementById('press-modal-ru-chips');
    modalReportingUnits = [...(shape.config.reporting_units || [])];
    renderRuChips();
    ddPressRu.input.value = '';
    document.getElementById('dd-press-plc').classList.remove('open');
    document.getElementById('dd-press-count-tag').classList.remove('open');
    document.getElementById('dd-press-job-count-tag').classList.remove('open');
    document.getElementById('dd-press-spm-tag').classList.remove('open');
    document.getElementById('dd-press-coil-tag').classList.remove('open');
    document.getElementById('dd-press-state-tag').classList.remove('open');
    document.getElementById('dd-press-style-tag').classList.remove('open');
    document.getElementById('dd-press-cat-id-1').classList.remove('open');
    document.getElementById('dd-press-cat-id-2').classList.remove('open');
    document.getElementById('dd-press-cat-id-3').classList.remove('open');
    document.getElementById('dd-press-cat-id-4').classList.remove('open');
    document.getElementById('dd-press-cat-id-5').classList.remove('open');
    document.getElementById('dd-press-ru').classList.remove('open');
    pressModalOverlay.classList.add('open');
  }

  function closePressModal(save) {
    if (save && modalShapeId != null) {
      const s = state.shapes.find(sh => sh.id === modalShapeId);
      if (s && s.config) {
        s.config.backgroundColor = selectedBgColor;
        s.config.plc = ddPressPLC.input.value;
        s.config.count_tag = ddPressCountTag.input.value;
        s.config.job_count_tag = ddPressJobCountTag.input.value;
        s.config.spm_tag = ddPressSpmTag.input.value;
        s.config.coil_tag = ddPressCoilTag.input.value;
        s.config.state_tag = ddPressStateTag.input.value;
        s.config.style_tag = ddPressStyleTag.input.value;
        s.config.cat_id_1 = ddPressCatId1.input.value;
        s.config.cat_id_2 = ddPressCatId2.input.value;
        s.config.cat_id_3 = ddPressCatId3.input.value;
        s.config.cat_id_4 = ddPressCatId4.input.value;
        s.config.cat_id_5 = ddPressCatId5.input.value;
        s.config.styles = modalStyles.filter(st => st.name);
        s.config.reporting_units = [...modalReportingUnits];
      }
      saveLayoutQuiet();
    }
    pressModalOverlay.classList.remove('open');
    modalShapeId = null;
  }

  wireModal('press-modal-overlay', 'press-modal-save', 'press-modal-cancel', closePressModal);

  // --- Buffer modal ---
  let bufferModalShapeId = null;

  function openBufferModal(shape) {
    bufferModalShapeId = shape.id;
    ddBufPLC.input.value = shape.config.plc || '';
    updateTagDropdownsForPlc(ddBufPLC.input.value, ddBufTag);
    ddBufTag.input.value = shape.config.tag || '';
    document.getElementById('dd-buf-plc').classList.remove('open');
    document.getElementById('dd-buf-tag').classList.remove('open');
    bufferModalOverlay.classList.add('open');
  }

  function closeBufferModal(save) {
    if (save && bufferModalShapeId != null) {
      const s = state.shapes.find(sh => sh.id === bufferModalShapeId);
      if (s && s.config) {
        s.config.plc = ddBufPLC.input.value;
        s.config.tag = ddBufTag.input.value;
      }
    }
    bufferModalOverlay.classList.remove('open');
    bufferModalShapeId = null;
  }

  wireModal('buffer-modal-overlay', 'buffer-modal-save', 'buffer-modal-cancel', closeBufferModal);

  // --- Multi Buffer modal ---
  let mbufModalShapeId = null;

  function openMultiBufferModal(shape) {
    mbufModalShapeId = shape.id;
    ddMbufPLC.input.value = shape.config.plc || '';
    updateTagDropdownsForPlc(ddMbufPLC.input.value, ddMbufCountTag);
    ddMbufCountTag.input.value = shape.config.count_tag || '';
    document.getElementById('dd-mbuf-plc').classList.remove('open');
    document.getElementById('dd-mbuf-count-tag').classList.remove('open');
    mbufModalOverlay.classList.add('open');
  }

  function closeMultiBufferModal(save) {
    if (save && mbufModalShapeId != null) {
      const s = state.shapes.find(sh => sh.id === mbufModalShapeId);
      if (s && s.config) {
        s.config.plc = ddMbufPLC.input.value;
        s.config.count_tag = ddMbufCountTag.input.value;
      }
    }
    mbufModalOverlay.classList.remove('open');
    mbufModalShapeId = null;
  }

  wireModal('mbuf-modal-overlay', 'mbuf-modal-save', 'mbuf-modal-cancel', closeMultiBufferModal);

  // --- Final modal ---
  let finalModalShapeId = null;

  function openFinalModal(shape) {
    finalModalShapeId = shape.id;
    ddFinalPLC.input.value = shape.config.plc || '';
    updateTagDropdownsForPlc(ddFinalPLC.input.value, ddFinalCountTag);
    ddFinalCountTag.input.value = shape.config.count_tag || '';
    document.getElementById('dd-final-plc').classList.remove('open');
    document.getElementById('dd-final-count-tag').classList.remove('open');
    finalModalOverlay.classList.add('open');
  }

  function closeFinalModal(save) {
    if (save && finalModalShapeId != null) {
      const s = state.shapes.find(sh => sh.id === finalModalShapeId);
      if (s && s.config) {
        s.config.plc = ddFinalPLC.input.value;
        s.config.count_tag = ddFinalCountTag.input.value;
      }
    }
    finalModalOverlay.classList.remove('open');
    finalModalShapeId = null;
  }

  wireModal('final-modal-overlay', 'final-modal-save', 'final-modal-cancel', closeFinalModal);

  function isAnyModalOpen() {
    return processModalOverlay.classList.contains('open')
      || pressModalOverlay.classList.contains('open')
      || bufferModalOverlay.classList.contains('open')
      || mbufModalOverlay.classList.contains('open')
      || finalModalOverlay.classList.contains('open');
  }

  return {
    openProcessModal,
    openPressModal,
    openBufferModal,
    openMultiBufferModal,
    openFinalModal,
    isAnyModalOpen,
    initWarlink,
    fetchReportingUnits,
  };
}
