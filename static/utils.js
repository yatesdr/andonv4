// utils.js — Shared utility functions loaded by non-module templates.
// Included via <script src="/static/utils.js"> before inline scripts.

/**
 * esc escapes a string for safe inclusion in HTML.
 * @param {string} s - The string to escape.
 * @returns {string} HTML-escaped string, or empty string if falsy.
 */
function esc(s) {
  if (!s) return '';
  return String(s).replace(/&/g, '&amp;').replace(/"/g, '&quot;')
    .replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

/** Return "YYYY-MM-DD" in the browser's local timezone. */
function localDateStr(d) {
  if (!d) d = new Date();
  return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0');
}

/** Parse a "YYYY-MM-DD" string as local midnight (avoids the UTC foot-gun of new Date("YYYY-MM-DD")). */
function parseLocalDate(s) {
  var p = s.split('-');
  return new Date(+p[0], +p[1] - 1, +p[2]);
}
