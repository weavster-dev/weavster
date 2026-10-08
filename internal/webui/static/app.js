// Weavster read-only topology UI. The pure functions below (layout and
// rendering) are exported as WeavsterUI and tested from Go; the browser
// wiring at the end runs only where there is a document. The UI never
// changes anything: it signs in and out, and reads the topology.
(function (root) {
  'use strict';

  var POLL_MS = 5000;
  var NODE_W = 190, NODE_H = 58, COL_GAP = 70, ROW_GAP = 22, PAD = 20;

  function esc(s) {
    return String(s === undefined || s === null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }

  // parseRoute reads the location hash: #/ is the overview, #/flows/<id>
  // one flow.
  function parseRoute(hash) {
    var m = /^#\/flows\/(.+)$/.exec(hash || '');
    if (m) {
      var id = m[1];
      try {
        id = decodeURIComponent(id);
      } catch (e) { /* a malformed escape: use the text as it is */ }
      return { view: 'flow', id: id };
    }
    return { view: 'overview' };
  }

  // topologyPath is the API path of a route's graph.
  function topologyPath(route) {
    return route.view === 'flow' ? 'topology/flows/' + encodeURIComponent(route.id) : 'topology';
  }

  // flowLink is the hash of a flow node's drill-down (flow:<id> -> #/flows/<id>).
  function flowLink(nodeID) {
    return '#/flows/' + encodeURIComponent(nodeID.replace(/^flow:/, ''));
  }

  // layout places nodes in columns by their longest path from a node
  // without incoming edges, along route and message-path edges
  // (dependencies are drawn but do not rank). A node is placed once all
  // edges into it are; when only cycles are left, the first node left in
  // the graph's order starts, and edges back into placed nodes are ignored.
  function layout(graph) {
    var nodes = graph.nodes || [], edges = graph.edges || [];
    // Prototype-free maps: ids come from the server and may be any text.
    var rank = Object.create(null), indeg = Object.create(null), out = Object.create(null), done = Object.create(null);
    var queue = [], placed = 0, i, j;
    for (i = 0; i < nodes.length; i++) {
      rank[nodes[i].id] = 0;
      indeg[nodes[i].id] = 0;
      out[nodes[i].id] = [];
    }
    for (j = 0; j < edges.length; j++) {
      var e = edges[j];
      if (e.kind !== 'dependency' && e.from in rank && e.to in rank && e.from !== e.to) {
        out[e.from].push(e.to);
        indeg[e.to]++;
      }
    }
    for (i = 0; i < nodes.length; i++) {
      if (indeg[nodes[i].id] === 0) {
        queue.push(nodes[i].id);
      }
    }
    while (placed < nodes.length) {
      if (!queue.length) { // only cycles are left
        for (i = 0; i < nodes.length && done[nodes[i].id]; i++) { /* the first node left */ }
        queue.push(nodes[i].id);
      }
      var u = queue.shift();
      if (done[u]) {
        continue;
      }
      done[u] = true;
      placed++;
      for (j = 0; j < out[u].length; j++) {
        var v = out[u][j];
        if (done[v]) {
          continue;
        }
        rank[v] = Math.max(rank[v], rank[u] + 1);
        if (--indeg[v] === 0) {
          queue.push(v);
        }
      }
    }
    var cols = [], pos = Object.create(null), maxRank = 0, maxRows = 0, r;
    for (i = 0; i < nodes.length; i++) {
      r = rank[nodes[i].id];
      (cols[r] = cols[r] || []).push(nodes[i].id);
      maxRank = Math.max(maxRank, r);
    }
    for (r = 0; r < cols.length; r++) {
      var col = cols[r] || [];
      maxRows = Math.max(maxRows, col.length);
      for (j = 0; j < col.length; j++) {
        pos[col[j]] = { x: PAD + r * (NODE_W + COL_GAP), y: PAD + j * (NODE_H + ROW_GAP) };
      }
    }
    return {
      pos: pos,
      width: nodes.length ? PAD * 2 + (maxRank + 1) * NODE_W + maxRank * COL_GAP : 0,
      height: nodes.length ? PAD * 2 + maxRows * NODE_H + (maxRows - 1) * ROW_GAP : 0
    };
  }

  function activityText(a) {
    if (!a) {
      return '';
    }
    return a.received + ' in · ' + a.sent + ' sent · ' + a.errored + ' err · ' + a.queued + ' queued';
  }

  function nodeSVG(n, p) {
    var body = '<g class="node kind-' + esc(n.kind) + ' status-' + esc(n.status || 'none') + '" data-id="' + esc(n.id) + '"' +
      ' transform="translate(' + p.x + ',' + p.y + ')">' +
      '<title>' + esc(n.id) + '</title>' +
      '<rect width="' + NODE_W + '" height="' + NODE_H + '" rx="6"></rect>' +
      '<text x="10" y="18">' + esc(n.label || n.id) + '</text>' +
      '<text x="10" y="34" class="muted">' + esc(n.kind + (n.status ? ' · ' + n.status : '')) + '</text>' +
      '<text x="10" y="50" class="muted">' + esc(activityText(n.activity)) + '</text></g>';
    if (n.kind === 'flow') {
      return '<a href="' + esc(flowLink(n.id)) + '">' + body + '</a>';
    }
    return body;
  }

  function edgeSVG(e, pos) {
    var a = pos[e.from], b = pos[e.to];
    if (!a || !b) {
      return '';
    }
    var x1 = a.x + NODE_W, y1 = a.y + NODE_H / 2, x2 = b.x, y2 = b.y + NODE_H / 2;
    if (e.from === e.to) { // a flow routing to itself: a loop on its right
      return '<path class="edge kind-' + esc(e.kind) + ' status-' + esc(e.status || 'none') + '" d="M' + x1 + ',' + (a.y + 14) +
        ' C' + (x1 + 45) + ',' + (a.y - 10) + ' ' + (x1 + 45) + ',' + (a.y + NODE_H + 10) + ' ' + x1 + ',' + (a.y + NODE_H - 14) +
        '"><title>' + esc(edgeTitle(e)) + '</title></path>';
    }
    if (x2 <= x1) { // same column or backwards: loop around below
      x1 = a.x + NODE_W / 2; y1 = a.y + NODE_H; x2 = b.x + NODE_W / 2; y2 = b.y + NODE_H;
      return '<path class="edge kind-' + esc(e.kind) + ' status-' + esc(e.status || 'none') + '" d="M' + x1 + ',' + y1 +
        ' C' + x1 + ',' + (y1 + 40) + ' ' + x2 + ',' + (y2 + 40) + ' ' + x2 + ',' + y2 + '"><title>' + esc(edgeTitle(e)) + '</title></path>';
    }
    var mx = (x1 + x2) / 2;
    return '<path class="edge kind-' + esc(e.kind) + ' status-' + esc(e.status || 'none') + '" d="M' + x1 + ',' + y1 +
      ' C' + mx + ',' + y1 + ' ' + mx + ',' + y2 + ' ' + x2 + ',' + y2 + '"><title>' + esc(edgeTitle(e)) + '</title></path>';
  }

  function edgeTitle(e) {
    return e.kind + (e.label ? ' ' + e.label : '') + (e.status ? ' (' + e.status + ')' : '') +
      (e.activity ? ': ' + activityText(e.activity) : '');
  }

  // renderGraph draws a graph as SVG.
  function renderGraph(graph) {
    var l = layout(graph), out = [], i;
    out.push('<svg xmlns="http://www.w3.org/2000/svg" width="' + l.width + '" height="' + (l.height + 40) + '" role="img" aria-label="topology graph">' +
      '<defs><marker id="arrow" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">' +
      '<path d="M0,0 L10,5 L0,10 z" fill="#8a8a95"></path></marker></defs>');
    for (i = 0; i < graph.edges.length; i++) {
      out.push(edgeSVG(graph.edges[i], l.pos));
    }
    for (i = 0; i < graph.nodes.length; i++) {
      out.push(nodeSVG(graph.nodes[i], l.pos[graph.nodes[i].id]));
    }
    out.push('</svg>');
    return out.join('');
  }

  // renderView is the main area for a state: {phase: loading | error |
  // ready, route, graph, error, updated}.
  function renderView(state) {
    var route = state.route || { view: 'overview' };
    var back = route.view === 'flow' ? '<a href="#/">← All flows</a>' : '';
    if (state.phase === 'loading') {
      return back + '<p class="state">Loading…</p>';
    }
    if (state.phase === 'error') {
      return back + '<p class="state error" role="alert">' + esc(state.error) + '</p>';
    }
    var g = state.graph;
    var title = route.view === 'flow'
      ? '<h2>' + esc(g.flowName || String(g.flowId || route.id).replace(/^flow:/, '')) + '</h2><span>' + esc(g.flowStatus || '') + '</span>'
      : '<h2>All flows</h2>';
    var bar = '<div class="bar">' + back + title + '<span class="updated">updated ' + esc(state.updated || g.generatedAt) +
      ', refreshed every ' + POLL_MS / 1000 + ' s</span></div>';
    if (!g.nodes.length) {
      var empty = route.view === 'flow'
        ? 'This flow has no source, transform, or destinations.'
        : 'No flows yet. Create them with the API or weavster config apply; this page only shows them.';
      return bar + '<p class="state">' + esc(empty) + '</p>';
    }
    var legend = route.view === 'overview' ? 'Select a flow to see its source, transform, and destinations.' : '';
    return bar + '<div class="graph">' + renderGraph(g) + '</div><p class="legend">' + esc(legend) + '</p>';
  }

  // loginView is the sign-in form.
  function loginView(message) {
    return '<form class="login" id="login"><h2>Sign in</h2>' +
      (message ? '<p class="state error" role="alert">' + esc(message) + '</p>' : '') +
      '<label>User name <input name="username" autocomplete="username" required></label>' +
      '<label>Password <input name="password" type="password" autocomplete="current-password" required></label>' +
      '<button type="submit">Sign in</button></form>';
  }

  // errorText explains a failed request.
  function errorText(status, route, body) {
    if (status === 403 && body && body.error && body.error.code === 'PASSWORD_CHANGE_REQUIRED') {
      return 'Change your password first (weavster CLI or POST /api/v1/auth/password), then sign in again.';
    }
    if (status === 403) {
      return 'Your account needs the flows:view permission to see the topology.';
    }
    if (status === 404 && route.view === 'flow') {
      return 'There is no flow ' + route.id + '.';
    }
    var msg = body && body.error && body.error.message ? ': ' + body.error.message : '';
    return 'The server answered ' + status + msg + '.';
  }

  root.WeavsterUI = {
    esc: esc, parseRoute: parseRoute, topologyPath: topologyPath, flowLink: flowLink, layout: layout,
    activityText: activityText, renderGraph: renderGraph, renderView: renderView, loginView: loginView,
    errorText: errorText, POLL_MS: POLL_MS
  };

  if (typeof document === 'undefined') {
    return;
  }

  // Browser wiring.
  var API = '../api/v1/';
  var main = document.getElementById('main');
  var signout = document.getElementById('signout');
  var timer = null, seq = 0;
  var shown = null; // the route key and view (minus its update time) on screen

  function token() {
    try { return sessionStorage.getItem('weavster.token'); } catch (e) { return null; }
  }
  function setToken(t) {
    try {
      if (t) { sessionStorage.setItem('weavster.token', t); } else { sessionStorage.removeItem('weavster.token'); }
    } catch (e) { /* storage unavailable: the token lasts until reload */ }
    memoryToken = t;
  }
  var memoryToken = token();

  function request(method, path, body) {
    var headers = { 'X-Weavster-CSRF': '1', 'Accept': 'application/json' };
    if (memoryToken) {
      headers.Authorization = 'Bearer ' + memoryToken;
    }
    if (body) {
      headers['Content-Type'] = 'application/json';
    }
    return fetch(API + path, { method: method, headers: headers, body: body ? JSON.stringify(body) : undefined, credentials: 'omit' })
      .then(function (res) {
        return res.json().catch(function () { return null; }).then(function (json) { return { status: res.status, body: json }; });
      });
  }

  function showLogin(message) {
    clearTimeout(timer);
    seq++; // a request still under way is not shown
    shown = null;
    signout.hidden = true;
    main.innerHTML = loginView(message);
    document.getElementById('login').addEventListener('submit', function (ev) {
      ev.preventDefault();
      var f = ev.target;
      request('POST', 'auth/login', { username: f.username.value, password: f.password.value }).then(function (r) {
        if (r.status !== 200 || !r.body || !r.body.token) {
          showLogin(r.status === 401 ? 'Wrong user name or password.' : errorText(r.status, { view: 'overview' }, r.body));
          return;
        }
        setToken(r.body.token);
        load();
      }, function () { showLogin('The server cannot be reached.'); });
    });
  }

  function load() {
    clearTimeout(timer);
    if (!memoryToken) {
      showLogin('');
      return;
    }
    var route = parseRoute(location.hash), mine = ++seq, key = topologyPath(route);
    if (!shown || shown.key !== key) { // a new view: say it is loading
      main.innerHTML = renderView({ phase: 'loading', route: route });
      shown = { key: key, html: '' };
    }
    signout.hidden = false;
    request('GET', topologyPath(route)).then(function (r) {
      if (mine !== seq) {
        return; // a newer load is under way
      }
      if (r.status === 401) {
        setToken(null);
        showLogin('Your session ended. Sign in again.');
        return;
      }
      var state = r.status === 200
        ? { phase: 'ready', route: route, graph: r.body, updated: new Date().toLocaleTimeString() }
        : { phase: 'error', route: route, error: errorText(r.status, route, r.body) };
      show(key, renderView(state));
      schedule();
    }, function () {
      if (mine === seq) {
        show(key, renderView({ phase: 'error', route: route, error: 'The server cannot be reached; retrying.' }));
        schedule();
      }
    });
  }

  // show puts a view on screen. When only its update time changed, just
  // that is updated, so scrolling, focus, and tooltips stay; otherwise the
  // graph's scroll position is kept.
  var UPDATED = /<span class="updated">[^<]*<\/span>/;
  function show(key, html) {
    var same = html.replace(UPDATED, '');
    if (shown && shown.key === key && shown.html === same) {
      var m = UPDATED.exec(html), el = main.querySelector('.updated');
      if (m && el) {
        el.outerHTML = m[0];
      }
      return;
    }
    var box = main.querySelector('.graph');
    var left = box ? box.scrollLeft : 0, top = box ? box.scrollTop : 0;
    var focused = document.activeElement && main.contains(document.activeElement) ? document.activeElement.getAttribute('href') : null;
    main.innerHTML = html;
    box = main.querySelector('.graph');
    if (box && shown && shown.key === key) {
      box.scrollLeft = left;
      box.scrollTop = top;
      if (focused) { // the same link keeps the keyboard focus
        var links = main.querySelectorAll('a[href]');
        for (var i = 0; i < links.length; i++) {
          if (links[i].getAttribute('href') === focused) {
            links[i].focus();
            break;
          }
        }
      }
    }
    shown = { key: key, html: same };
  }

  function schedule() {
    clearTimeout(timer);
    timer = setTimeout(function () {
      if (document.hidden) {
        schedule(); // no polling in a background tab
      } else {
        load();
      }
    }, POLL_MS);
  }

  signout.addEventListener('click', function () {
    seq++; // a poll under way is not shown
    clearTimeout(timer);
    request('POST', 'auth/logout').then(function () {}, function () {}).then(function () {
      setToken(null);
      showLogin('');
    });
  });
  window.addEventListener('hashchange', load);
  load();
}(this));
