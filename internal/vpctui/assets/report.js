// VPC report behaviour. Everything here is optional: the report is complete
// and readable with JavaScript off — tables still show every row, the diagram
// still draws, and its layer checkboxes still work (they are pure CSS). This
// only adds the conveniences that need a script.
(function () {
  "use strict";
  var root = document.documentElement;

  // Theme, remembered per browser. Without this the page follows the OS.
  try {
    var saved = localStorage.getItem("aws-explorer-theme");
    if (saved === "light" || saved === "dark") root.setAttribute("data-theme", saved);
  } catch (e) {}
  var btn = document.getElementById("themebtn");
  if (btn) {
    btn.hidden = false;
    btn.addEventListener("click", function () {
      var dark = root.getAttribute("data-theme") === "dark" ||
        (!root.getAttribute("data-theme") && window.matchMedia &&
          matchMedia("(prefers-color-scheme: dark)").matches);
      var next = dark ? "light" : "dark";
      root.setAttribute("data-theme", next);
      try { localStorage.setItem("aws-explorer-theme", next); } catch (e) {}
    });
  }

  // Sortable columns. A column counts as numeric only when every non-empty
  // cell in it parses as a number, so an ID column that happens to start with
  // digits is still sorted as text.
  function numericColumn(rows, idx) {
    var seen = 0;
    for (var i = 0; i < rows.length; i++) {
      var cell = rows[i].cells[idx];
      if (!cell) continue;
      var t = cell.textContent.trim().replace(/,/g, "");
      if (t === "" || t === "—" || t === "-") continue;
      if (!/^-?\d+(\.\d+)?$/.test(t)) return false;
      seen++;
    }
    return seen > 0;
  }

  document.querySelectorAll(".tbl table").forEach(function (table) {
    var body = table.tBodies[0];
    if (!body || body.rows.length < 2) return;
    var rows = Array.prototype.slice.call(body.rows);
    var heads = table.querySelectorAll("thead th");

    heads.forEach(function (th, idx) {
      var numeric = numericColumn(rows, idx);
      th.setAttribute("data-sort", numeric ? "num" : "text");
      th.setAttribute("tabindex", "0");
      th.setAttribute("role", "button");
      th.setAttribute("aria-sort", "none");
      function sort() {
        var dir = th.getAttribute("data-dir") === "desc" ? "asc" : "desc";
        heads.forEach(function (h) { h.removeAttribute("data-dir"); h.setAttribute("aria-sort", "none"); });
        th.setAttribute("data-dir", dir);
        th.setAttribute("aria-sort", dir === "asc" ? "ascending" : "descending");
        var current = Array.prototype.slice.call(body.rows);
        current.sort(function (a, b) {
          var x = a.cells[idx], y = b.cells[idx];
          var av = x ? x.textContent.trim() : "";
          var bv = y ? y.textContent.trim() : "";
          var c = numeric
            ? (parseFloat(av.replace(/,/g, "")) || 0) - (parseFloat(bv.replace(/,/g, "")) || 0)
            : av.localeCompare(bv, undefined, { numeric: true });
          return dir === "asc" ? c : -c;
        });
        current.forEach(function (r) { body.appendChild(r); });
      }
      th.addEventListener("click", sort);
      th.addEventListener("keydown", function (e) {
        if (e.key === "Enter" || e.key === " ") { e.preventDefault(); sort(); }
      });
    });
  });

  // Per-table filter. The box is hidden in the markup and revealed here, so it
  // never appears without the script that makes it work.
  document.querySelectorAll(".tblbar").forEach(function (bar) {
    var input = bar.querySelector(".filter");
    var count = bar.querySelector(".count");
    var wrap = bar.nextElementSibling;
    if (!input || !wrap || !wrap.classList.contains("tbl")) return;
    var body = wrap.querySelector("tbody");
    if (!body) return;
    input.hidden = false;
    var total = body.rows.length;
    input.addEventListener("input", function () {
      var q = input.value.trim().toLowerCase();
      var shown = 0;
      Array.prototype.forEach.call(body.rows, function (tr) {
        var hit = q === "" || tr.textContent.toLowerCase().indexOf(q) >= 0;
        tr.hidden = !hit;
        if (hit) shown++;
      });
      if (count) {
        count.textContent = q === ""
          ? total + (total === 1 ? " row" : " rows")
          : shown + " of " + total + (total === 1 ? " row" : " rows");
      }
    });
  });

  // Highlight the section the reader is in, so a long inventory doesn't lose
  // its place in the sidebar.
  if (window.IntersectionObserver) {
    var links = {};
    document.querySelectorAll("nav.toc a").forEach(function (a) {
      var id = a.getAttribute("href");
      if (id && id.charAt(0) === "#") links[id.slice(1)] = a;
    });
    var seen = new Set();
    var obs = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) seen.add(e.target.id); else seen.delete(e.target.id);
      });
      var first = null;
      Object.keys(links).forEach(function (id) {
        links[id].classList.remove("on");
        if (!first && seen.has(id)) first = id;
      });
      if (first) links[first].classList.add("on");
    }, { rootMargin: "-10% 0px -70% 0px" });
    document.querySelectorAll("main section[id], main h2[id]").forEach(function (el) {
      if (el.id && links[el.id]) obs.observe(el);
    });
  }
})();
