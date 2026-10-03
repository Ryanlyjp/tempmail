'use strict';

// Hints only: browsers and password-manager extensions may override them.
(() => {
  const selector = 'form, input, textarea, select';
  function mark(element) {
    element.setAttribute('autocomplete', element.matches('input[type="password"]') ? 'new-password' : 'off');
    if (!element.matches('form')) {
      element.setAttribute('data-1p-ignore', 'true');
      element.setAttribute('data-lpignore', 'true');
      element.setAttribute('data-bwignore', 'true');
      element.setAttribute('data-protonpass-ignore', 'true');
    }
  }
  function scan(root) {
    if (root.nodeType !== Node.ELEMENT_NODE) return;
    if (root.matches(selector)) mark(root);
    root.querySelectorAll(selector).forEach(mark);
  }
  scan(document.documentElement);
  new MutationObserver(records => {
    for (const record of records) {
      if (record.type === 'attributes') mark(record.target);
      else record.addedNodes.forEach(scan);
    }
  }).observe(document.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ['type'] });
})();
