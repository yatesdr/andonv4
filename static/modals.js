// Shared modal utilities for the designer page.

// Creates a filter dropdown (search + select) bound to a wrapper element.
// Returns { input, setOptions }.
export function initFilterDropdown(wrapperId, initialOptions) {
  const wrap = document.getElementById(wrapperId);
  const input = wrap.querySelector('input');
  const list = wrap.querySelector('.dd-list');
  let options = initialOptions || [];

  function render(filter) {
    const f = (filter || '').toLowerCase();
    list.innerHTML = '';
    const matches = options.filter(o => o.toLowerCase().includes(f));
    if (matches.length === 0) {
      list.innerHTML = '<div class="dd-item" style="color:#999;pointer-events:none;">No matches</div>';
      return;
    }
    matches.forEach(opt => {
      const div = document.createElement('div');
      div.className = 'dd-item';
      div.textContent = opt;
      div.addEventListener('mousedown', e => {
        e.preventDefault();
        input.value = opt;
        wrap.classList.remove('open');
        input.dispatchEvent(new Event('change'));
      });
      list.appendChild(div);
    });
  }

  input.addEventListener('focus', () => {
    render(input.value);
    wrap.classList.add('open');
  });
  input.addEventListener('input', () => {
    render(input.value);
    wrap.classList.add('open');
  });
  input.addEventListener('blur', () => {
    setTimeout(() => wrap.classList.remove('open'), 150);
  });

  function setOptions(newOpts) {
    options = newOpts || [];
    if (wrap.classList.contains('open')) render(input.value);
  }

  return { input, setOptions };
}

// Wires standard modal dismiss behaviour: save button, cancel button,
// overlay click to close, Escape key to close.
// closeFn(save: boolean) is called on each action.
export function wireModal(overlayId, saveId, cancelId, closeFn) {
  const overlay = document.getElementById(overlayId);
  document.getElementById(saveId).addEventListener('click', () => closeFn(true));
  document.getElementById(cancelId).addEventListener('click', () => closeFn(false));
  overlay.addEventListener('click', e => {
    if (e.target === overlay) closeFn(false);
  });
  window.addEventListener('keydown', e => {
    if (overlay.classList.contains('open') && e.key === 'Escape') {
      closeFn(false);
      e.preventDefault();
    }
  });
  return overlay;
}
