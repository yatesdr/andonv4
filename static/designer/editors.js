export function initEditors(state, deps) {
  const { getLabelBounds } = deps;
  const labelEdit = document.getElementById('label-edit');
  const SCALE = state.SCALE;

  let editingHeader = null;
  let editingStatus = null;
  let editingChild = null;

  function openHeaderTextEditor(shape) {
    const c = shape.config;
    const tx = c.x + (c.textX || 0.5) * c.w;
    const ty = c.y + (c.textY || 0.5) * c.h;
    const fontSize = (c.h * 0.9) / SCALE;
    labelEdit.style.display = 'block';
    labelEdit.style.left = (tx / SCALE) + 'px';
    labelEdit.style.top = (ty / SCALE) + 'px';
    labelEdit.style.fontSize = fontSize + 'px';
    labelEdit.style.color = '#000';
    labelEdit.value = c.text || '';
    editingHeader = shape.id;
    labelEdit.focus();
    labelEdit.select();
  }

  function closeHeaderTextEditor(save) {
    if (editingHeader == null) return;
    if (save) {
      const s = state.shapes.find(sh => sh.id === editingHeader);
      if (s) s.config.text = labelEdit.value || s.config.text;
    }
    labelEdit.style.display = 'none';
    editingHeader = null;
  }

  function openStatusTextEditor(shape) {
    const c = shape.config;
    const cssX = (c.x + c.w / 2) / SCALE;
    const cssY = (c.y + c.h / 2) / SCALE;
    const fontSize = (c.h * 0.6) / SCALE;
    labelEdit.style.display = 'block';
    labelEdit.style.left = cssX + 'px';
    labelEdit.style.top = cssY + 'px';
    labelEdit.style.fontSize = fontSize + 'px';
    labelEdit.style.color = '#000';
    labelEdit.value = c.text || '';
    editingStatus = shape.id;
    labelEdit.focus();
    labelEdit.select();
  }

  function closeStatusTextEditor(save) {
    if (editingStatus == null) return;
    if (save) {
      const s = state.shapes.find(sh => sh.id === editingStatus);
      if (s) s.config.text = labelEdit.value || s.config.text;
    }
    labelEdit.style.display = 'none';
    editingStatus = null;
  }

  function openLabelEditor(shape, child) {
    const b = getLabelBounds(state.ctx, shape, child);
    const cssX = b.cx / SCALE;
    const cssY = b.cy / SCALE;
    const fontSize = Math.max(12, (shape.config.h * 0.28) * 1.0 * (child.scale || 1)) / SCALE;

    labelEdit.style.display = 'block';
    labelEdit.style.left = cssX + 'px';
    labelEdit.style.top = cssY + 'px';
    labelEdit.style.fontSize = fontSize + 'px';
    labelEdit.style.color = child.color || '#000';
    labelEdit.value = child.text || 'OP-100';
    editingChild = { shapeId: shape.id, childId: child.id };

    labelEdit.focus();
    labelEdit.select();
  }

  function closeLabelEditor(save) {
    if (!editingChild) return;
    if (save) {
      const s = state.shapes.find(sh => sh.id === editingChild.shapeId);
      if (s && s.config.children) {
        const child = s.config.children.find(c => c.id === editingChild.childId);
        if (child) child.text = labelEdit.value || child.text;
      }
    }
    labelEdit.style.display = 'none';
    editingChild = null;
  }

  function isEditing() {
    return editingChild != null || editingHeader != null || editingStatus != null;
  }

  // Wire up label-edit keyboard and blur events
  labelEdit.addEventListener('keydown', e => {
    if (e.key === 'Enter') { closeLabelEditor(true); closeHeaderTextEditor(true); closeStatusTextEditor(true); }
    if (e.key === 'Escape') { closeLabelEditor(false); closeHeaderTextEditor(false); closeStatusTextEditor(false); }
    e.stopPropagation();
  });

  labelEdit.addEventListener('blur', () => {
    closeLabelEditor(true);
    closeHeaderTextEditor(true);
    closeStatusTextEditor(true);
  });

  return {
    openHeaderTextEditor,
    closeHeaderTextEditor,
    openStatusTextEditor,
    closeStatusTextEditor,
    openLabelEditor,
    closeLabelEditor,
    isEditing,
  };
}
