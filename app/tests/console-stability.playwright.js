// Playwright CLI: run-code --filename app/tests/console-stability.playwright.js
// Requires running R0/R1. Exercises config-mode painter scripting, concurrent
// read-only console input, and control keys without changing device config.
async (page) => {
  await page.addInitScript(() => {
    const state={armed:false,injections:0,consoleText:'',responses:[],closes:0};
    window.__consoleStabilityProbe=state;
    const Native=window.WebSocket;
    window.WebSocket=class extends Native {
      constructor(...args){
        super(...args);
        if(String(args[0]).endsWith('/console/1')){
          this.addEventListener('close',()=>state.closes++);
          this.addEventListener('message',event=>{
            if(!(event.data instanceof ArrayBuffer))return;
            state.consoleText=(state.consoleText+new TextDecoder().decode(event.data)).slice(-1048576);
            if(state.armed&&state.consoleText.includes('do terminal length 0')){
              state.armed=false;state.injections++;
              // This is the same binary console-input path used by keystrokes.
              this.send(new TextEncoder().encode('do show clock\r'));
            }
          });
        }
        if(String(args[0]).endsWith('/control'))this.addEventListener('message',event=>{
          for(const line of String(event.data).split('\n')){
            try{const frame=JSON.parse(line);if(frame.ok&&frame.result?.proto)state.responses.push(frame);}catch{}
          }
        });
      }
    };
  });
  await page.reload();
  await page.getByRole('button',{name:'Stop lab',exact:true}).waitFor();
  await page.getByRole('button',{name:'Handle Handle Handle Handle Node quick actions Connect this node Running R1',exact:true}).dblclick();
  const r1=page.getByRole('dialog',{name:'R1',exact:true});
  await r1.getByRole('textbox',{name:'Terminal input'}).waitFor();
  const title=await r1.getByRole('toolbar',{name:'R1 window controls'}).boundingBox();
  await page.mouse.move(title.x+100,title.y+10);await page.mouse.down();
  await page.mouse.move(800,title.y+10,{steps:10});await page.mouse.up();
  await r1.locator('.xterm-screen').click({position:{x:10,y:10}});
  await page.waitForTimeout(100);
  const input=r1.getByRole('textbox',{name:'Terminal input'});
  const waitPrompt=async config=>{
    await page.waitForFunction(config=>{
      const raw=window.__consoleStabilityProbe.consoleText;
      if(raw)return config?/R1\(config\)#\s*$/.test(raw):/R1#\s*$/.test(raw);
      const rows=document.querySelector('[role=dialog][aria-label=R1] .xterm-rows');
      const tail=rows?.innerText.trim().split('\n').at(-1)?.trim();
      return config?tail==='R1(config)#':tail==='R1#';
    },config,{timeout:30000});
  };
  await input.press('Enter');await page.waitForTimeout(100);
  const tail=(await r1.locator('.xterm-rows').innerText()).trim().split('\n').at(-1)?.trim();
  if(tail==='R1>'){await input.pressSequentially('enable');await input.press('Enter');await waitPrompt(false);}
  if(tail!=='R1(config)#'){
    await input.pressSequentially('configure terminal');await input.press('Enter');await waitPrompt(true);
  }
  await r1.getByRole('button',{name:'Minimize window',exact:true}).click();
  await page.getByRole('button',{name:'Tools',exact:true}).click();
  const tools=page.getByRole('dialog',{name:'Tools',exact:true});
  await tools.getByRole('button',{name:'Topology painter',exact:true}).click();
  await tools.getByRole('button',{name:'Close',exact:true}).click();
  const panel=page.getByRole('dialog',{name:'Topology Painter',exact:true});
  const outcomes=[];
  for(const proto of ['OSPF','EIGRP','BGP']){
    await panel.getByRole('radio',{name:proto,exact:true}).click();
    await panel.getByRole('textbox',{name:'Destination prefix or host'}).fill('203.0.113.0/24');
    await page.evaluate(()=>{const s=window.__consoleStabilityProbe;s.armed=true;s.injections=0;s.consoleText='';s.responses=[];s.closes=0;});
    await panel.getByRole('button',{name:/^(Paint|Re-paint)$/}).click();
    await page.waitForFunction(()=>window.__consoleStabilityProbe.responses.length>0&&!document.querySelector('.pp-run')?.textContent.includes('Painting'),null,{timeout:30000});
    await page.waitForFunction(()=>/do show clock\r?\n[^\r\n]*\d{2}:\d{2}:\d{2}/.test(window.__consoleStabilityProbe.consoleText),null,{timeout:30000});
    await waitPrompt(true);
    const outcome=await page.evaluate(()=>{
      const s=window.__consoleStabilityProbe;
      const firstShow=s.consoleText.indexOf('do show ');
      const clock=s.consoleText.indexOf('do show clock');
      const preceding=s.consoleText.slice(firstShow,clock);
      return{injections:s.injections,closes:s.closes,clockAfterShow:firstShow>=0&&clock>firstShow&&preceding.includes('\r\nR1(config)#'),snapshot:s.responses.at(-1).result};
    });
    if(outcome.injections!==1||outcome.closes!==0||!outcome.clockAfterShow||outcome.snapshot.proto!==proto.toLowerCase()||outcome.snapshot.dest!=='203.0.113.0/24')throw new Error(`Bad ${proto} scripted/input ordering`);
    const node=outcome.snapshot.nodes.find(n=>n.node===1);
    if(!node?.running||/timeout|failed|error|unavailable/i.test(node.hint||''))throw new Error(`Failed ${proto} node collection`);
    outcomes.push({proto,injections:outcome.injections,clockAfterShow:outcome.clockAfterShow,nodes:outcome.snapshot.nodes.length});
  }
  await panel.getByRole('button',{name:'Close',exact:true}).click();
  await page.getByRole('button',{name:'Console R1',exact:true}).click();
  await r1.locator('.xterm-screen').click({position:{x:10,y:10}});await page.waitForTimeout(50);
  const controlKeys=[];
  for(const key of ['Control+c','Control+z']){
    if(key==='Control+z'){
      await input.pressSequentially('configure terminal');await input.press('Enter');await waitPrompt(true);
    }
    await page.evaluate(()=>window.__consoleStabilityProbe.consoleText='');
    await input.press(key);await input.press('Enter');
    await page.waitForFunction(()=>/R1(?:\(config\))?#\s*$/.test(window.__consoleStabilityProbe.consoleText),null,{timeout:30000});
    await page.waitForTimeout(100);
    const config=await page.evaluate(()=>/R1\(config\)#\s*$/.test(window.__consoleStabilityProbe.consoleText));
    const command=config?'do show clock':'show clock';
    await page.evaluate(()=>window.__consoleStabilityProbe.consoleText='');
    await input.pressSequentially(command);await input.press('Enter');
    await page.waitForFunction(()=>/show clock\r?\n[^\r\n]*\d{2}:\d{2}:\d{2}/.test(window.__consoleStabilityProbe.consoleText),null,{timeout:30000});
    await waitPrompt(config);
    if(await page.evaluate(()=>window.__consoleStabilityProbe.closes))throw new Error(`Console disconnected after ${key}`);
    await page.getByRole('button',{name:'Handle Handle Handle Handle Node quick actions Connect this node Running R1',exact:true}).waitFor();
    controlKeys.push({key,mode:config?'config':'exec',clockResult:true});
  }
  const beforePrompts=await r1.locator('.xterm-rows').evaluate(rows=>[...rows.querySelectorAll('span')].filter(s=>s.textContent==='R1#').length);
  for(let i=0;i<10;i++)await input.press('Enter');
  await page.waitForTimeout(150);
  const prompts=await r1.locator('.xterm-rows').evaluate(rows=>[...rows.querySelectorAll('span')].filter(s=>s.textContent==='R1#').map(s=>getComputedStyle(s).color));
  if(prompts.length-beforePrompts<5||prompts.slice(beforePrompts).some(c=>c!=='rgb(0, 255, 255)'))throw new Error('Rapid Enter prompt color lost');
  await page.screenshot({path:'scratch/console-stability-final.png'});
  return{passed:true,outcomes,controlKeys,ctrlCAndCtrlZ:'fresh clock results without disconnect, node still running',rapidEnterPrompts:prompts.length-beforePrompts};
}
