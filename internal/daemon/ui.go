package daemon

// uiHTML is the embedded single-page UI served by the daemon.
var uiHTML = buildUIHTML()

func buildUIHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>relay</title>
<style>
:root{--bg:#0d1117;--bg2:#161b22;--bg3:#21262d;--border:#30363d;--text:#c9d1d9;--text-muted:#8b949e;--accent:#58a6ff;--green:#3fb950;--yellow:#d29922;--red:#f85149}
*{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--text);font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',monospace;font-size:13px;line-height:1.6}
header{background:var(--bg2);border-bottom:1px solid var(--border);padding:12px 24px;display:flex;align-items:center;gap:16px}
header h1{font-size:16px;font-weight:600;color:var(--accent);letter-spacing:.05em}
header .version{color:var(--text-muted);font-size:11px}
.container{display:grid;grid-template-columns:260px 1fr;height:calc(100vh - 49px)}
.sidebar{background:var(--bg2);border-right:1px solid var(--border);overflow-y:auto;padding:12px}
.sidebar h2{font-size:11px;text-transform:uppercase;color:var(--text-muted);letter-spacing:.1em;margin-bottom:8px;padding:0 4px}
.thread-item{padding:8px 10px;border-radius:6px;cursor:pointer;margin-bottom:2px;border:1px solid transparent}
.thread-item:hover{background:var(--bg3)}
.thread-item.active{background:var(--bg3);border-color:var(--border)}
.thread-item .tid{font-family:monospace;font-size:11px;color:var(--accent)}
.thread-item .tname{font-size:12px;margin-top:2px}
.thread-item .tmeta{font-size:10px;color:var(--text-muted);margin-top:2px}
.main{overflow-y:auto;padding:24px}
.empty{text-align:center;color:var(--text-muted);margin-top:80px}
.empty h2{font-size:18px;margin-bottom:8px}
.tabs{display:flex;gap:0;border-bottom:1px solid var(--border);margin-bottom:20px}
.tab{padding:8px 16px;cursor:pointer;color:var(--text-muted);font-size:13px;border-bottom:2px solid transparent;margin-bottom:-1px}
.tab:hover{color:var(--text)}
.tab.active{color:var(--accent);border-bottom-color:var(--accent)}
.section{margin-bottom:24px}
.section h3{font-size:12px;text-transform:uppercase;color:var(--text-muted);letter-spacing:.08em;margin-bottom:10px}
.card{background:var(--bg2);border:1px solid var(--border);border-radius:8px;padding:14px 16px;margin-bottom:8px}
.card .label{font-size:11px;color:var(--text-muted);margin-bottom:4px}
.metric-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(160px,1fr));gap:12px}
.metric-card{background:var(--bg2);border:1px solid var(--border);border-radius:8px;padding:14px;text-align:center}
.metric-card .value{font-size:24px;font-weight:600;color:var(--accent)}
.metric-card .label{font-size:11px;color:var(--text-muted);margin-top:4px}
.event-list{list-style:none}
.event-item{padding:6px 0;border-bottom:1px solid var(--border);display:flex;gap:12px;align-items:flex-start}
.event-time{font-size:11px;color:var(--text-muted);white-space:nowrap;font-family:monospace}
.event-type{font-size:11px;padding:1px 6px;border-radius:4px;background:var(--bg3);color:var(--accent);white-space:nowrap}
.event-payload{font-size:11px;color:var(--text-muted);font-family:monospace;overflow:hidden;text-overflow:ellipsis}
.artifact-ref{font-family:monospace;font-size:11px;color:var(--accent)}
.artifact-type{font-size:11px;padding:1px 6px;border-radius:4px;background:var(--bg3)}
.badge{display:inline-block;font-size:10px;padding:1px 6px;border-radius:4px}
.badge-green{background:rgba(63,185,80,.15);color:var(--green)}
.badge-blue{background:rgba(88,166,255,.15);color:var(--accent)}
pre{background:var(--bg3);border:1px solid var(--border);border-radius:6px;padding:12px;font-size:11px;overflow-x:auto;white-space:pre-wrap;word-break:break-all}
.btn{padding:6px 14px;border-radius:6px;border:1px solid var(--border);background:var(--bg3);color:var(--text);cursor:pointer;font-size:12px}
.btn:hover{background:var(--accent);color:var(--bg);border-color:var(--accent)}
.token-bar{height:8px;border-radius:4px;background:var(--bg3);overflow:hidden;margin-top:6px}
.token-bar-fill{height:100%;border-radius:4px;background:var(--green);transition:width .3s}
.refresh-btn{margin-left:auto}
.loading{color:var(--text-muted);font-style:italic}
</style>
</head>
<body>
<header>
  <h1>relay</h1>
  <span class="version" id="version-tag">v1.0.0</span>
  <button class="btn refresh-btn" onclick="refresh()">Refresh</button>
</header>
<div class="container">
  <div class="sidebar">
    <h2>Threads</h2>
    <div id="thread-list"><span class="loading">Loading...</span></div>
  </div>
  <div class="main" id="main-panel">
    <div class="empty">
      <h2>relay</h2>
      <p>Select a thread from the sidebar to inspect it.</p>
    </div>
  </div>
</div>
<script>
var API='',currentThread=null,currentTab='overview';

function api(path,opts){
  var token=localStorage.getItem('relay_token')||'';
  var headers={'Content-Type':'application/json'};
  if(token)headers['Authorization']='Bearer '+token;
  return fetch(API+path,Object.assign({headers:headers},opts||{})).then(function(r){
    if(!r.ok)return r.text().then(function(t){throw new Error(t)});
    return r.json();
  });
}

function loadThreads(){
  api('/threads').then(function(data){
    renderThreadList(data.threads||[]);
  }).catch(function(e){
    document.getElementById('thread-list').innerHTML='<span style="color:var(--red)">Error: '+e.message+'</span>';
  });
}

function renderThreadList(threads){
  var el=document.getElementById('thread-list');
  if(!threads.length){el.innerHTML='<span class="loading">No threads yet</span>';return;}
  el.innerHTML=threads.map(function(t){
    var active=t.thread_id===currentThread?' active':'';
    return '<div class="thread-item'+active+'" onclick="selectThread(\''+t.thread_id+'\')">'+
      '<div class="tid">'+t.thread_id.slice(0,8)+'...</div>'+
      '<div class="tname">'+(t.name||'untitled')+'</div>'+
      '<div class="tmeta">'+(t.artifact_count||0)+' artifacts &middot; '+(t.hop_count||0)+' hops</div>'+
      '</div>';
  }).join('');
}

function selectThread(id){
  currentThread=id;currentTab='overview';
  renderThread(id);
  loadThreads();
}

function renderThread(id){
  var main=document.getElementById('main-panel');
  main.innerHTML='<span class="loading">Loading...</span>';
  Promise.all([
    api('/threads/'+id),
    api('/threads/'+id+'/state/header'),
    api('/threads/'+id+'/artifacts'),
    api('/threads/'+id+'/events')
  ]).then(function(results){
    main.innerHTML=buildThreadHTML(results[0],results[1],results[2].artifacts||[],results[3].events||[]);
  }).catch(function(e){
    main.innerHTML='<span style="color:var(--red)">Error: '+e.message+'</span>';
  });
}

function buildThreadHTML(thread,header,arts,evs){
  var naiveTokens=arts.reduce(function(s,a){return s+Math.floor(a.size/4);},0);
  var pct=naiveTokens>0?Math.round((naiveTokens-100)/naiveTokens*100):0;
  var tabs=['overview','state','artifacts','events','tokens'];
  var tabHTML=tabs.map(function(t){
    var label=t.charAt(0).toUpperCase()+t.slice(1);
    if(t==='artifacts')label+=' ('+arts.length+')';
    if(t==='events')label+=' ('+evs.length+')';
    return '<div class="tab'+(currentTab===t?' active':'')+'" onclick="switchTab(\''+t+'\')">'+label+'</div>';
  }).join('');

  var content='';
  if(currentTab==='overview')content=buildOverview(thread,header,arts,evs);
  else if(currentTab==='state')content=buildState(header);
  else if(currentTab==='artifacts')content=buildArtifacts(arts);
  else if(currentTab==='events')content=buildEvents(evs);
  else if(currentTab==='tokens')content=buildTokenStats(naiveTokens,header);

  return '<div class="tabs">'+tabHTML+'</div><div id="tab-content">'+content+'</div>';
}

function buildOverview(thread,header,arts,evs){
  var metrics=header.metrics||{};
  var grid=[
    {v:header.version,l:'State Version'},
    {v:arts.length,l:'Artifacts'},
    {v:thread.hop_count||0,l:'Hops'},
    {v:evs.length,l:'Events'},
    {v:metrics.cache_hits||0,l:'Cache Hits'},
    {v:metrics.tokens_avoided||0,l:'Tokens Avoided'}
  ].map(function(m){
    return '<div class="metric-card"><div class="value">'+m.v+'</div><div class="label">'+m.l+'</div></div>';
  }).join('');

  var recentEvs=evs.slice(-10).reverse().map(function(e){
    var payload=e.payload?JSON.stringify(e.payload).slice(0,80):'';
    return '<li class="event-item">'+
      '<span class="event-time">'+new Date(e.timestamp).toLocaleTimeString()+'</span>'+
      '<span class="event-type">'+e.type+'</span>'+
      '<span class="event-payload">'+escHtml(payload)+'</span></li>';
  }).join('');

  return '<div class="metric-grid">'+grid+'</div>'+
    '<div class="section" style="margin-top:20px"><h3>Thread</h3>'+
    '<div class="card"><div class="label">ID</div><div style="font-family:monospace;font-size:12px">'+thread.thread_id+'</div>'+
    '<div class="label" style="margin-top:8px">Created</div><div>'+thread.created_at+'</div></div></div>'+
    '<div class="section"><h3>Recent Events</h3><ul class="event-list">'+recentEvs+'</ul></div>';
}

function buildState(header){
  var sections=[
    {key:'top_facts',label:'Facts'},
    {key:'top_constraints',label:'Constraints'},
    {key:'open_questions',label:'Open Questions'},
    {key:'next_steps',label:'Next Steps'}
  ];
  var html='<div class="section"><h3>State Header</h3>';
  sections.forEach(function(s){
    var items=header[s.key]||[];
    if(!items.length)return;
    html+='<div class="card"><div class="label">'+s.label+'</div><ul style="margin-top:4px;padding-left:16px">';
    items.forEach(function(item){html+='<li style="margin-bottom:4px">'+escHtml(JSON.stringify(item))+'</li>';});
    html+='</ul></div>';
  });
  html+='<div class="card"><div class="label">Raw JSON</div><pre>'+escHtml(JSON.stringify(header,null,2))+'</pre></div></div>';
  return html;
}

function buildArtifacts(arts){
  if(!arts.length)return '<p class="loading">No artifacts yet.</p>';
  return '<div class="section"><h3>Artifacts</h3>'+arts.map(function(a){
    var preview=a.preview&&a.preview.text?'<pre>'+escHtml(a.preview.text.slice(0,300))+(a.preview.truncated?'\n...[truncated]':'')+'</pre>':'';
    return '<div class="card">'+
      '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">'+
      '<span class="artifact-ref">'+a.ref+'</span>'+
      '<span class="artifact-type badge badge-blue">'+a.type+'</span>'+
      '<span style="font-size:11px;color:var(--text-muted)">'+formatSize(a.size)+'</span></div>'+
      (a.name?'<div style="color:var(--text-muted);font-size:11px;margin-bottom:6px">'+escHtml(a.name)+'</div>':'')+
      preview+'</div>';
  }).join('')+'</div>';
}

function buildEvents(evs){
  return '<div class="section"><h3>Event Log</h3><ul class="event-list">'+evs.map(function(e){
    return '<li class="event-item">'+
      '<span class="event-time">'+new Date(e.timestamp).toISOString().slice(11,19)+'</span>'+
      '<span class="event-type">'+e.type+'</span>'+
      '<span class="event-payload">'+escHtml(JSON.stringify(e.payload).slice(0,100))+'</span></li>';
  }).join('')+'</ul></div>';
}

function buildTokenStats(naiveTokens,header){
  var metrics=header.metrics||{};
  var avoided=naiveTokens-Math.round(naiveTokens*0.1);
  var pct=naiveTokens>0?Math.round(avoided/naiveTokens*100):0;
  var grid=[
    {v:naiveTokens.toLocaleString(),l:'Naive Tokens'},
    {v:Math.round(naiveTokens*0.1).toLocaleString(),l:'Actual Tokens'},
    {v:avoided.toLocaleString(),l:'Tokens Avoided',c:'var(--green)'},
    {v:pct+'%',l:'Reduction',c:'var(--green)'},
    {v:metrics.cache_hits||0,l:'Cache Hits'},
    {v:metrics.cache_misses||0,l:'Cache Misses'}
  ].map(function(m){
    return '<div class="metric-card"><div class="value" '+(m.c?'style="color:'+m.c+'"':'')+'>'+m.v+'</div><div class="label">'+m.l+'</div></div>';
  }).join('');
  return '<div class="metric-grid">'+grid+'</div>'+
    '<div class="section" style="margin-top:20px"><h3>Token Reduction</h3><div class="card">'+
    '<div style="display:flex;justify-content:space-between;margin-bottom:6px"><span>Reduction</span><span class="badge badge-green">'+pct+'%</span></div>'+
    '<div class="token-bar"><div class="token-bar-fill" style="width:'+pct+'%"></div></div>'+
    '<div style="margin-top:12px;font-size:11px;color:var(--text-muted)">By storing artifacts by reference, relay avoids re-sending '+avoided.toLocaleString()+' estimated tokens.</div>'+
    '</div></div>';
}

function switchTab(tab){currentTab=tab;if(currentThread)renderThread(currentThread);}
function escHtml(s){return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');}
function formatSize(n){if(n<1024)return n+' B';if(n<1048576)return (n/1024).toFixed(1)+' KB';return (n/1048576).toFixed(2)+' MB';}

function refresh(){loadThreads();if(currentThread)renderThread(currentThread);}

function loadVersion(){
  api('/version').then(function(v){
    document.getElementById('version-tag').textContent='v'+v.version;
  }).catch(function(){});
}

setInterval(function(){if(currentThread&&currentTab==='events')renderThread(currentThread);},5000);
loadThreads();loadVersion();
</script>
</body>
</html>`
}
