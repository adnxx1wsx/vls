// Vless 流量审计 — Dashboard App
const API = '/api';
const TOKEN = localStorage.getItem('auth_token');
if (!TOKEN) { window.location.href = '/app/login.html?redirect=/app/'; }

async function apiFetch(url, opts = {}) {
  opts.headers = opts.headers || {};
  opts.headers['X-Auth-Token'] = TOKEN;
  const res = await fetch(url, opts);
  if (res.status === 401) {
    localStorage.removeItem('auth_token');
    window.location.href = '/app/login.html?redirect=/app/';
    throw new Error('unauthorized');
  }
  return res;
}

// ── Formatting ──
function fmtBytes(b) { if(!b||b===0)return'0 B';const u=['B','KB','MB','GB','TB'];let i=0,v=b;while(v>=1024&&i<u.length-1){v/=1024;i++;}return v.toFixed(i>0?1:0)+' '+u[i]; }
function fmtDuration(ms){if(!ms||ms===0)return'--';if(ms<1000)return ms+'ms';const s=ms/1000;if(s<60)return s.toFixed(1)+'s';const m=Math.floor(s/60);return m+'m '+(s%60).toFixed(0)+'s';}
function fmtTime(ts){return new Date(ts).toLocaleTimeString('zh-CN',{hour12:false});}

// ── Source / Target display ──
function fmtSource(c){
  const ip=c.source||'--';
  if(c.source_country==='CDN') return `${ip} (CDN节点)`;
  const loc=[c.source_region,c.source_city].filter(Boolean).join(' ');
  return loc?`${ip} · ${loc}`:ip;
}
function fmtTarget(c){
  const d=c.target_domain||'';
  const t=c.target||'--';
  return d?`${d} (${t})`:t;
}

// ── Chart ──
let trafficChart;
function initChart(){
  trafficChart=new Chart(document.getElementById('trafficChart'),{
    type:'line',
    data:{labels:[],datasets:[
      {label:'上行',data:[],borderColor:'#34d399',backgroundColor:'rgba(52,211,153,0.05)',fill:true,tension:.4,pointRadius:0,borderWidth:2},
      {label:'下行',data:[],borderColor:'#60a5fa',backgroundColor:'rgba(96,165,250,0.05)',fill:true,tension:.4,pointRadius:0,borderWidth:2},
    ]},
    options:{responsive:true,maintainAspectRatio:false,
      plugins:{legend:{labels:{color:'#5c617a',usePointStyle:true,boxWidth:8,padding:16,font:{size:11}}}},
      scales:{
        x:{ticks:{color:'#5c617a',maxTicksLimit:10,font:{size:10}},grid:{color:'#1c1f2e',drawBorder:false}},
        y:{ticks:{color:'#5c617a',font:{size:10},callback:v=>fmtBytes(v),maxTicksLimit:4},grid:{color:'#1c1f2e',drawBorder:false}},
      },
      interaction:{intersect:false,mode:'index'},
    }
  });
}

async function updateChart(){
  try{
    const r=await apiFetch(`${API}/stats/realtime?minutes=60`);
    const d=await r.json();const b=d.buckets||[];
    trafficChart.data.labels=b.map(x=>new Date(x.time).toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit'}));
    trafficChart.data.datasets[0].data=b.map(x=>x.up);
    trafficChart.data.datasets[1].data=b.map(x=>x.down);
    trafficChart.update('none');
  }catch(_){}
}

// ── Rate tracking ──
let lastUp=0,lastDown=0;
async function updateRates(){
  try{
    const r=await apiFetch(`${API}/traffic/summary?period=24h`);
    const d=await r.json();
    document.getElementById('today-up').textContent=fmtBytes(d.up_bytes);
    document.getElementById('today-down').textContent=fmtBytes(d.down_bytes);
    const upRate=d.up_bytes-lastUp,downRate=d.down_bytes-lastDown;
    if(lastUp>0) document.getElementById('rate-up').textContent=`+${fmtBytes(upRate)}/5s`;
    if(lastDown>0) document.getElementById('rate-down').textContent=`+${fmtBytes(downRate)}/5s`;
    lastUp=d.up_bytes;lastDown=d.down_bytes;
    document.getElementById('conn-today').textContent = d.total_conns || '--';
  }catch(_){}
}

// ── Top Users / Targets ──
async function updateTopUsers(){
  try{
    const r=await apiFetch(`${API}/traffic/top?period=24h&limit=10`);
    const d=await r.json();const users=d.users||[];
    const max=Math.max(1,...users.map(u=>u.UpBytes+u.DownBytes));
    document.getElementById('top-users').innerHTML=users.map((u,i)=>`
      <div class="rank-row">
        <div class="rank-num">${i+1}</div>
        <div class="rank-info">
          <div class="rank-name">${u.UserEmail||'未知'}</div>
          <div class="rank-stat">↑${fmtBytes(u.UpBytes)} ↓${fmtBytes(u.DownBytes)}</div>
          <div class="rank-bar"><div class="rank-bar-inner" style="width:${((u.UpBytes+u.DownBytes)/max*100).toFixed(0)}%"></div></div>
        </div>
      </div>`).join('')||'<div style="color:var(--muted);padding:20px;text-align:center">暂无数据</div>';
  }catch(_){}
}

async function updateTopTargets(){
  try{
    const r=await apiFetch(`${API}/targets/top?period=24h&limit=15`);
    const d=await r.json();const t=d.targets||[];
    const max=Math.max(1,...t.map(x=>x.Count));
    document.getElementById('top-targets').innerHTML=t.map((x,i)=>`
      <div class="rank-row">
        <div class="rank-num">${i+1}</div>
        <div class="rank-info">
          <div class="rank-name" title="${x.Target}">${(x.TargetDomain||x.Target).slice(0,38)}</div>
          <div class="rank-stat">${x.Count} 次连接</div>
          <div class="rank-bar"><div class="rank-bar-inner" style="width:${(x.Count/max*100).toFixed(0)}%"></div></div>
        </div>
      </div>`).join('')||'<div style="color:var(--muted);padding:20px;text-align:center">暂无数据</div>';
  }catch(_){}
}

// ── Connections Table ──
let connPage=0;const connLimit=30;
async function loadConnections(){
  const user=document.getElementById('user-filter').value;
  const search=document.getElementById('search-target').value;
  const offset=connPage*connLimit;
  try{
    const p=new URLSearchParams({limit:connLimit,offset});
    if(user)p.set('user',user);
    const r=await apiFetch(`${API}/connections?${p}`);
    const d=await r.json();
    let conns=d.connections||[];
    if(search){conns=conns.filter(c=>(c.target||'').includes(search)||(c.target_domain||'').includes(search));}
    document.getElementById('conn-tbody').innerHTML=conns.map(c=>`
      <tr>
        <td>${fmtTime(c.timestamp)}</td>
        <td><span class="user-link" onclick="showUserDetail('${(c.user_email||'').replace(/'/g,"\\'")}')">${c.user_email||'--'}</span></td>
        <td>${fmtSource(c)}</td>
        <td title="${fmtTarget(c)}">${fmtTarget(c).slice(0,50)}</td>
        <td>${c.protocol}</td>
        <td>${fmtBytes(c.up_bytes)}</td>
        <td>${fmtBytes(c.down_bytes)}</td>
        <td>${fmtDuration(c.duration_ms)}</td>
        <td class="status-${c.status||'ok'}">${c.status||'ok'}</td>
      </tr>`).join('')||'<tr><td colspan="9" style="color:var(--muted);text-align:center;padding:30px">暂无连接记录</td></tr>';
    renderPagination(d.total);
  }catch(_){}
}
function renderPagination(total){
  const pages=Math.max(1,Math.ceil(total/connLimit));
  let h='';
  h+=`<button ${connPage===0?'disabled':''} onclick="goPage(${connPage-1})">◀</button>`;
  for(let i=0;i<Math.min(pages,10);i++) h+=`<button class="${i===connPage?'active':''}" onclick="goPage(${i})">${i+1}</button>`;
  h+=`<button ${connPage>=pages-1?'disabled':''} onclick="goPage(${connPage+1})">▶</button>`;
  document.getElementById('pagination').innerHTML=h;
}
function goPage(p){connPage=Math.max(0,p);loadConnections();}

// ── Users ──
async function loadUsers(){
  try{
    const r=await apiFetch(`${API}/users`);
    const d=await r.json();const sel=document.getElementById('user-filter');
    (d.users||[]).forEach(u=>{const o=document.createElement('option');o.value=u;o.textContent=u;sel.appendChild(o);});
  }catch(_){}
}

// ── Online ──
async function updateOnline(){
  try{
    const r=await apiFetch(`${API}/online?minutes=5`);
    const d=await r.json();
    document.getElementById('online-count').textContent=(d.online||[]).length;
  }catch(_){}
}

// ── Storage ──
async function updateStorage(){
  try{
    const r=await apiFetch(`${API}/stats/storage`);
    const d=await r.json();
    const size=d.db_size?(d.db_size/1024/1024).toFixed(1)+' MB':'--';
    document.getElementById('storage-info').textContent = size;
    document.getElementById('storage-footer').textContent = `共 ${d.connections||0} 条连接 · ${d.snapshots||0} 条流量快照 · 归档 ${d.archived||0} 条 · 数据库 ${size} · 保留 365 天`;
  }catch(_){}
}

// ── SSE ──
function setupSSE(){
  const es=new EventSource(`${API}/events/stream?token=${encodeURIComponent(TOKEN)}`);
  es.onmessage=e=>{
    try{
      const msg=JSON.parse(e.data);
      if(msg.type==='connection'){
        const tbody=document.getElementById('conn-tbody');
        const row=document.createElement('tr');
        row.innerHTML=`<td>${fmtTime(msg.timestamp)}</td>
          <td><span class="user-link" onclick="showUserDetail('${(msg.user_email||'').replace(/'/g,"\\'")}')">${msg.user_email||'--'}</span></td>
          <td>${fmtSource(msg)}</td><td title="${fmtTarget(msg)}">${fmtTarget(msg).slice(0,50)}</td>
          <td>${msg.protocol}</td><td>${fmtBytes(msg.up_bytes)}</td><td>${fmtBytes(msg.down_bytes)}</td>
          <td>${fmtDuration(msg.duration_ms)}</td><td class="status-${msg.status||'ok'}">${msg.status||'ok'}</td>`;
        tbody.insertBefore(row,tbody.firstChild);
        while(tbody.children.length>50)tbody.lastChild.remove();
      }
      if(msg.type==='traffic_summary'){
        document.getElementById('today-up').textContent=fmtBytes(msg.up_bytes);
        document.getElementById('today-down').textContent=fmtBytes(msg.down_bytes);
      }
    }catch(_){}
  };
}

// ── Tabs ──
function switchTab(tab){
  document.querySelectorAll('.tabs button').forEach(b=>b.classList.remove('active'));
  event.target.classList.add('active');
  document.getElementById('top-users').style.display=tab==='users'?'':'none';
  document.getElementById('top-targets').style.display=tab==='targets'?'':'none';
  if(tab==='targets')updateTopTargets();
}

// ── User Detail Modal ──
let modalChart=null;
async function showUserDetail(email){
  if(!email)return;
  document.getElementById('modal-title').textContent='👤 '+email;
  document.getElementById('user-modal').style.display='flex';
  try{
    const r=await apiFetch(`${API}/users/${encodeURIComponent(email)}/traffic?minutes=60`);
    const d=await r.json();const b=d.buckets||[];
    const ctx=document.getElementById('modalChart').getContext('2d');
    if(modalChart)modalChart.destroy();
    modalChart=new Chart(ctx,{type:'line',
      data:{labels:b.map(x=>new Date(x.time).toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit'})),datasets:[
        {label:'上行',data:b.map(x=>x.up),borderColor:'#34d399',backgroundColor:'rgba(52,211,153,.05)',fill:true,tension:.4,pointRadius:0,borderWidth:2},
        {label:'下行',data:b.map(x=>x.down),borderColor:'#60a5fa',backgroundColor:'rgba(96,165,250,.05)',fill:true,tension:.4,pointRadius:0,borderWidth:2}]},
      options:{responsive:true,maintainAspectRatio:false,plugins:{legend:{labels:{color:'#5c617a',usePointStyle:true,boxWidth:6,font:{size:10}}}},scales:{x:{ticks:{color:'#5c617a',maxTicksLimit:8,font:{size:10}},grid:{color:'#1c1f2e'}},y:{ticks:{color:'#5c617a',font:{size:10},callback:v=>fmtBytes(v)},grid:{color:'#1c1f2e'}}}}});
  }catch(_){}
  try{
    const r=await apiFetch(`${API}/users/${encodeURIComponent(email)}/timeline?limit=100`);
    const d=await r.json();
    document.getElementById('modal-tbody').innerHTML=(d.connections||[]).map(c=>`
      <tr><td>${fmtTime(c.timestamp)}</td><td>${fmtSource(c)}</td><td title="${fmtTarget(c)}">${fmtTarget(c).slice(0,45)}</td><td>${c.protocol}</td><td>${fmtBytes(c.up_bytes)}</td><td>${fmtBytes(c.down_bytes)}</td></tr>
    `).join('')||'<tr><td colspan="6" style="color:var(--muted);text-align:center">暂无记录</td></tr>';
  }catch(_){}
}
function closeModal(){
  document.getElementById('user-modal').style.display='none';
  if(modalChart){modalChart.destroy();modalChart=null;}
}
document.addEventListener('click',e=>{if(e.target.id==='user-modal')closeModal();});

// ── Export ──
function doExport(){
  const user=document.getElementById('user-filter').value;
  let url='/api/export/connections?limit=10000';
  if(user)url+='&user='+encodeURIComponent(user);
  window.open(url,'_blank');
}

// ── Init ──
function init(){
  initChart();
  updateChart();updateTopUsers();updateRates();updateOnline();updateStorage();loadConnections();loadUsers();setupSSE();
  setInterval(updateChart,15000);setInterval(updateTopUsers,60000);setInterval(updateRates,30000);setInterval(updateOnline,15000);
  document.getElementById('refresh-btn').addEventListener('click',()=>{connPage=0;loadConnections();updateChart();updateTopUsers();updateRates();updateOnline();});
  document.getElementById('export-btn').addEventListener('click',doExport);
  document.getElementById('user-filter').addEventListener('change',()=>{connPage=0;loadConnections();});
}
document.addEventListener('DOMContentLoaded',init);
