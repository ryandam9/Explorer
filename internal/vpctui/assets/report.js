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


  // ---- the diagram, made explorable -------------------------------------
  // All of this is additive: the SVG is a complete, readable picture on its
  // own, and none of these handlers change what it says — they only let you
  // ask it one question at a time.
  (function diagram() {
    var svg = document.querySelector(".diagram svg.vpcd");
    var box = document.querySelector(".diagram");
    if (!svg || !box) return;
    svg.classList.add("live");

    // Which subnets each route table governs, carried in the SVG itself.
    var groups = {};
    try {
      var meta = svg.querySelector("#vpcd-graph");
      if (meta) groups = (JSON.parse(meta.textContent) || {}).rt || {};
    } catch (e) {}

    var edges = Array.prototype.slice.call(svg.querySelectorAll("[data-from]"));
    var nodes = Array.prototype.slice.call(svg.querySelectorAll("[data-node]"));

    // related(id) is what stays lit: the node itself (a subnet is drawn across
    // several layers, so one id can be several elements), everything one hop
    // away along a line, and — for a route table — every subnet it governs,
    // with their one-hop neighbours.
    //
    // One hop, deliberately. Following the lines onward instead lights the
    // whole diagram after two or three steps (a private subnet reaches its
    // NAT, which reaches the gateway, which reaches the internet), and a
    // highlight that dims nothing answers nothing. To follow a path, hover
    // the next node along.
    function related(id) {
      var seeds = {};
      seeds[id] = true;
      (groups[id] || []).forEach(function (s) { seeds[s] = true; });
      var keep = {};
      Object.keys(seeds).forEach(function (k) { keep[k] = true; });
      edges.forEach(function (e) {
        var f = e.getAttribute("data-from"), t = e.getAttribute("data-to");
        if (seeds[f] || seeds[t]) { keep[f] = true; keep[t] = true; }
      });
      return { keep: keep, seeds: seeds };
    }

    var current = null;
    var label = null;

    function clear() {
      current = null;
      svg.classList.remove("sel");
      nodes.concat(edges).forEach(function (el) { el.classList.remove("hot"); });
      if (label) label.textContent = "";
    }

    function select(id) {
      if (!id) { clear(); return; }
      if (current === id) return;
      current = id;
      var r = related(id);
      svg.classList.add("sel");
      nodes.forEach(function (el) {
        el.classList.toggle("hot", !!r.keep[el.getAttribute("data-node")]);
      });
      edges.forEach(function (el) {
        var f = el.getAttribute("data-from"), t = el.getAttribute("data-to");
        el.classList.toggle("hot", !!(r.seeds[f] || r.seeds[t]));
      });
      if (label) label.textContent = id;
    }

    nodes.forEach(function (el) {
      var id = el.getAttribute("data-node");
      el.addEventListener("mouseenter", function () { select(id); });
      el.addEventListener("focus", function () { select(id); });
      el.addEventListener("blur", clear);
      el.addEventListener("click", function (e) { e.stopPropagation(); jumpTo(id); });
      el.addEventListener("keydown", function (e) {
        if (e.key === "Enter" || e.key === " ") { e.preventDefault(); jumpTo(id); }
        if (e.key === "Escape") clear();
      });
    });
    svg.addEventListener("mouseleave", clear);

    // Clicking a node jumps to its row in the inventory below and flashes it:
    // the diagram says what connects to what, the table says everything else
    // about it.
    function jumpTo(id) {
      var target = null;
      var cells = document.querySelectorAll("main .tbl td");
      for (var i = 0; i < cells.length && !target; i++) {
        if (cells[i].textContent.trim() === id) target = cells[i].parentNode;
      }
      if (!target) return;
      document.querySelectorAll("tr.flash").forEach(function (tr) { tr.classList.remove("flash"); });
      target.scrollIntoView({ block: "center", behavior: "smooth" });
      // Restart the animation even if the same row is clicked twice.
      void target.offsetWidth;
      target.classList.add("flash");
    }

    // Pan and zoom by moving the viewBox, so nothing in the drawing has to
    // know about it. The page's own scrolling is left alone: a bare wheel
    // scrolls the page as usual, and zooming is on the buttons, a double
    // click, or ctrl/⌘+wheel.
    var vb = (svg.getAttribute("viewBox") || "").split(/\s+/).map(Number);
    if (vb.length !== 4 || vb.some(isNaN)) return;
    var home = vb.slice();
    var view = vb.slice();

    function apply() { svg.setAttribute("viewBox", view.join(" ")); }
    function zoom(factor, cx, cy) {
      var w = view[2] * factor, h = view[3] * factor;
      // Bounded so the diagram can't be lost off-screen or shrunk to a dot.
      if (w > home[2] * 4 || w < home[2] / 8) return;
      view[0] = cx - (cx - view[0]) * factor;
      view[1] = cy - (cy - view[1]) * factor;
      view[2] = w;
      view[3] = h;
      apply();
    }
    function center() { return [view[0] + view[2] / 2, view[1] + view[3] / 2]; }
    function svgPoint(e) {
      var r = svg.getBoundingClientRect();
      return [
        view[0] + ((e.clientX - r.left) / r.width) * view[2],
        view[1] + ((e.clientY - r.top) / r.height) * view[3]
      ];
    }

    var tools = document.querySelector(".dgtools");
    if (tools) {
      tools.hidden = false;
      label = tools.querySelector(".sel-name");
      tools.addEventListener("click", function (e) {
        var act = e.target.getAttribute && e.target.getAttribute("data-act");
        if (!act) return;
        var c = center();
        if (act === "in") zoom(0.8, c[0], c[1]);
        if (act === "out") zoom(1.25, c[0], c[1]);
        if (act === "reset") { view = home.slice(); apply(); clear(); }
      });
    }

    svg.addEventListener("wheel", function (e) {
      if (!e.ctrlKey && !e.metaKey) return; // let the page scroll
      e.preventDefault();
      var p = svgPoint(e);
      zoom(e.deltaY > 0 ? 1.1 : 0.9, p[0], p[1]);
    }, { passive: false });

    svg.addEventListener("dblclick", function (e) {
      var p = svgPoint(e);
      zoom(0.7, p[0], p[1]);
    });

    // Drag to pan. The pointer is captured only once it has actually moved:
    // capturing on pointerdown retargets the click that follows to the <svg>,
    // which silently breaks click-to-jump on every node. A drag also swallows
    // the click it ends with, so releasing the mouse over a card does not
    // jump to its row.
    var drag = null;
    var dragged = false;
    var dragSlop = 4;

    svg.addEventListener("pointerdown", function (e) {
      if (e.button !== 0) return;
      drag = { x: e.clientX, y: e.clientY, vx: view[0], vy: view[1], id: e.pointerId, live: false };
      dragged = false;
    });
    svg.addEventListener("pointermove", function (e) {
      if (!drag) return;
      if (!drag.live) {
        if (Math.abs(e.clientX - drag.x) + Math.abs(e.clientY - drag.y) < dragSlop) return;
        drag.live = true;
        dragged = true;
        box.classList.add("grabbing");
        svg.setPointerCapture(drag.id);
      }
      var r = svg.getBoundingClientRect();
      view[0] = drag.vx - ((e.clientX - drag.x) / r.width) * view[2];
      view[1] = drag.vy - ((e.clientY - drag.y) / r.height) * view[3];
      apply();
    });
    function endDrag(e) {
      if (!drag) return;
      if (drag.live && svg.hasPointerCapture(drag.id)) svg.releasePointerCapture(drag.id);
      drag = null;
      box.classList.remove("grabbing");
    }
    svg.addEventListener("pointerup", endDrag);
    svg.addEventListener("pointercancel", endDrag);
    svg.addEventListener("click", function (e) {
      if (!dragged) return;
      dragged = false;
      e.stopPropagation();
      e.preventDefault();
    }, true);
    box.classList.add("grab");
  })();

  // Pin the ID column beside the row counter on wide tables. The offset is
  // the counter column's real width, which only the browser knows, so it is
  // measured here and published to the stylesheet. A table narrow enough not
  // to scroll is left alone: pinning costs nothing visually but the class is
  // what the CSS keys off, and there is no reason to set it.
  function pinIDColumns() {
    document.querySelectorAll(".tbl").forEach(function (box) {
      var table = box.querySelector("table");
      if (!table || !table.tHead) return;
      var head = table.tHead.rows[0];
      if (!head || head.cells.length < 3) return; // a two-column table has nothing to pin
      box.classList.remove("pinned");
      box.style.removeProperty("--col1");
      if (box.scrollWidth <= box.clientWidth) return;
      box.style.setProperty("--col1", head.cells[0].getBoundingClientRect().width + "px");
      box.classList.add("pinned");
    });
  }
  pinIDColumns();
  var pinTimer = null;
  window.addEventListener("resize", function () {
    clearTimeout(pinTimer);
    pinTimer = setTimeout(pinIDColumns, 150);
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
