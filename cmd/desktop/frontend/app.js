'use strict';
const $ = id => document.getElementById(id);
const api = () => window.go.desktop.App;
let state, busy=false, notice='', qrKey='', devicesKey='', polling=false;
const actionButtons=['toggle','restart','refresh-code','configure'];
function showNotice(message){ notice=String(message||''); $('notice').textContent=notice; $('notice').hidden=!notice; }
async function act(fn){
  if(busy)return;
  busy=true;actionButtons.forEach(id=>$(id).disabled=true);
  try{await fn();showNotice('');}catch(e){showNotice(e instanceof Error?e.message:String(e));}
  finally{busy=false;await poll();}
}
function text(id,value){$(id).textContent=value;}
async function render(s){
  state=s;
  const online=s.codexOnline&&s.relayOnline;
  const title=s.stopping?'正在停止后台':online?'电脑已就绪':s.running?'正在连接…':'后台已停止';
  text('hero-title',title);text('side-status',title);
  $('hero-dot').classList.toggle('online',online);$('side-dot').classList.toggle('online',online);
  text('hero-description',online?'手机现在可以访问电脑上的会话':'启动后自动连接 Codex 和中转服务器');
  text('codex-status',s.codexOnline?'已连接':'未连接');text('relay-status',s.relayOnline?'已连接':'未连接');
  text('phone-status',`已配对 ${s.devices.length} 台`);
  text('relay-address',s.relayURL?`${s.relayURL} · ${s.projects} 个项目`:'尚未配置 Relay');
  $('setup').hidden=s.configured;
  text('toggle',s.running?'停止后台':'启动后台');
  $('toggle').disabled=busy||s.stopping||!s.configured;
  $('restart').disabled=busy||s.stopping||!s.configured;
  $('refresh-code').disabled=busy||!online||s.stopping;
  $('configure').disabled=busy;
  const code=s.linkCode||'';
  text('code',code||'— — — — — —');$('copy-code').disabled=!code;
  const remaining=Math.max(0,Math.ceil((Date.parse(s.linkExpires)-Date.now())/1000));
  text('expires',code?`${Math.floor(remaining/60)} 分 ${remaining%60} 秒后过期`:(online?'点击刷新，生成新的配对码':'启动并连接后获取配对码'));
  $('autostart').checked=s.autostart;$('keep-awake').checked=s.keepAwake;
  text('awake-status',s.awakeActive?'保持唤醒已生效，屏幕可以正常关闭':s.keepAwake?'后台启动后生效':'');
  if(!notice){$('notice').hidden=!s.error;$('notice').textContent=s.error||'';}
  const nextDevices=JSON.stringify(s.devices);
  if(devicesKey!==nextDevices){
    devicesKey=nextDevices;const list=$('device-list');list.replaceChildren();
    if(!s.devices.length){const p=document.createElement('p');p.textContent='还没有已配对设备。请在连接与配对页面连接手机。';list.append(p);}
    for(const id of s.devices){
      const row=document.createElement('div');row.className='device';
      const label=document.createElement('code');label.textContent=id;
      const remove=document.createElement('button');remove.textContent='移除设备';
      remove.onclick=()=>{if(window.confirm('移除该手机的访问权限？'))act(()=>api().RevokeDevice(id));};
      row.append(label,remove);list.append(row);
    }
  }
  const logs=$('log-output'),atEnd=logs.scrollHeight-logs.scrollTop-logs.clientHeight<40;
  const lines=(s.logs||[]).join('\n')||'暂无日志';
  if(logs.textContent!==lines){logs.textContent=lines;if(atEnd)logs.scrollTop=logs.scrollHeight;}
  const key=code?code+s.linkExpires:'';
  if(key!==qrKey){
    qrKey=key;$('qr').hidden=true;$('qr-placeholder').hidden=false;
    if(key){try{const image=await api().PairingQR();if(qrKey===key){$('qr').src=image;$('qr').hidden=false;$('qr-placeholder').hidden=true;}}catch(e){text('qr-placeholder','二维码暂不可用，请刷新配对码');}}
  }
}
async function poll(){
  if(polling)return;polling=true;
  try{await render(await api().GetState());}catch(e){showNotice('无法读取电脑后台状态：'+String(e));}finally{polling=false;}
}
document.querySelectorAll('.nav').forEach(button=>button.onclick=()=>{
  document.querySelectorAll('.nav').forEach(b=>b.classList.toggle('active',b===button));
  document.querySelectorAll('.page').forEach(p=>p.hidden=p.id!==button.dataset.page);
  text('page-title',button.textContent.trim().slice(1).trim());
});
$('toggle').onclick=()=>{if(!state.running||window.confirm('停止后台将断开手机连接，并可能中断正在执行的任务，继续？'))act(()=>state.running?api().Stop():api().Start());};
$('restart').onclick=()=>{if(!state.running||window.confirm('重启将断开手机连接，并可能中断正在执行的任务，继续？'))act(()=>api().Restart());};
$('refresh-code').onclick=()=>act(()=>api().RefreshPairing());
$('configure').onclick=()=>act(async()=>{await api().Configure($('relay').value.trim());await api().Start();});
$('autostart').onchange=e=>act(()=>api().SetAutostart(e.target.checked));
$('keep-awake').onchange=e=>act(()=>api().SetKeepAwake(e.target.checked));
$('copy-code').onclick=()=>act(()=>window.runtime.ClipboardSetText(state.linkCode));
$('quit').onclick=()=>{if(window.confirm('退出后手机将无法连接电脑，确定退出？'))api().Quit();};
poll();setInterval(poll,1200);
