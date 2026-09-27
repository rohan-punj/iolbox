// Playwright CLI: run-code --filename app/tests/console-performance.playwright.js
// Requires a running R0/R1 lab. Reads device output and changes only session
// exec/pager settings. Returned diagnostics contain no console text.
async (page) => {
  const wire = {0:{text:'',opens:0,closes:0},1:{text:'',opens:0,closes:0}};
  const onSocket = socket => {
    const match = socket.url().match(/\/console\/([01])$/);
    if (!match) return;
    const record = wire[match[1]];
    record.opens++;
    socket.on('close',()=>record.closes++);
    socket.on('framereceived',({payload})=>{record.text=(record.text+payload.toString()).slice(-1048576);});
  };
  page.on('websocket',onSocket);
  const errors=[];const onError = error => errors.push(error.message);
  page.on('pageerror',onError);
  try {
    await page.goto(page.url().split('?')[0]+'?consoleMetrics=1');
    await page.getByRole('button',{name:'Stop lab',exact:true}).waitFor();
    for(const name of ['R0','R1']) {
      const dialog=page.getByRole('dialog',{name,exact:true});
      if(!await dialog.count()) await page.getByRole('button',{name:`Handle Handle Handle Handle Node quick actions Connect this node Running ${name}`,exact:true}).dblclick();
      if((await dialog.getAttribute('class')).includes('minimized')) await page.getByRole('button',{name:`Console ${name}`,exact:true}).click();
      await dialog.getByRole('textbox',{name:'Terminal input'}).waitFor();
    }
    const r0=page.getByRole('dialog',{name:'R0',exact:true});
    const r1=page.getByRole('dialog',{name:'R1',exact:true});
    const title=await r0.getByRole('toolbar',{name:'R0 window controls'}).boundingBox();
    await page.mouse.move(title.x+100,title.y+10);await page.mouse.down();
    await page.mouse.move(800,title.y+10,{steps:10});await page.mouse.up();
    const snapshot=()=>page.evaluate(()=>window.__iolboxConsoleMetrics.snapshot());
    const activate=async dialog=>{
      await dialog.locator('.xterm-screen').click({position:{x:10,y:10}});
      const name=await dialog.getAttribute('aria-label');
      await page.waitForFunction(name=>document.activeElement?.classList.contains('xterm-helper-textarea')&&document.activeElement.closest('[role=dialog]')?.getAttribute('aria-label')===name,name);
    };
    const waitComplete=async(dialog,id,before)=>{
      const deadline=Date.now()+30000;let stable=null;let last=-1;
      while(Date.now()<deadline){
        const stats=(await snapshot()).nodes[id];
        const tail=(await dialog.locator('.xterm-rows').innerText()).trim().split('\n').at(-1)?.trim()||'';
        if(stats.wsChunks>before && /^\S+[>#]\s*$/.test(tail) && stats.backlogBytes===0 && stats.pendingReceiveBytes===0){
          if(last!==stats.wsChunks||stable===null)stable=Date.now();
          if(Date.now()-stable>=200)return;
        }else stable=null;
        last=stats.wsChunks;await page.waitForTimeout(40);
      }
      throw new Error(`Console ${id} did not reach a fresh settled prompt`);
    };
    const send=async(dialog,id,command)=>{
      await activate(dialog);
      const before=(await snapshot()).nodes[id].wsChunks;
      const input=dialog.getByRole('textbox',{name:'Terminal input'});
      await input.pressSequentially(command);await input.press('Enter');
      await waitComplete(dialog,id,before);
    };
    for(const [dialog,id] of [[r0,0],[r1,1]]){
      await activate(dialog);
      const input=dialog.getByRole('textbox',{name:'Terminal input'});
      // Process running does not mean IOS has finished its boot sequence.
      const readyDeadline=Date.now()+60000;
      let ready=false;
      while(Date.now()<readyDeadline){
        await input.press('Enter');await page.waitForTimeout(400);
        const tail=(await dialog.locator('.xterm-rows').innerText()).trim().split('\n').at(-1)?.trim()||'';
        if(/^\S+[>#]$/.test(tail)){ready=true;break;}
      }
      if(!ready)throw new Error(`IOS console ${id} not ready`);
      const tail=(await dialog.locator('.xterm-rows').innerText()).trim().split('\n').at(-1)||'';
      if(tail.includes('('))throw new Error('Run the measurement in exec mode');
      if(tail.endsWith('>'))await send(dialog,id,'enable');
      await send(dialog,id,'terminal length 0');
    }
    const results=[];
    for(const minimized of [false,true]){
      await page.evaluate(()=>window.__iolboxConsoleMetrics.reset());
      for(const record of Object.values(wire)){record.text='';record.opens=record.closes=0;}
      await activate(r0);
      const input0=r0.getByRole('textbox',{name:'Terminal input'});
      await input0.pressSequentially('show interfaces');
      const previous=(await snapshot()).nodes[0].wsChunks;
      if(minimized)await input0.evaluate(el=>el.addEventListener('keyup',()=>el.closest('[role=dialog]').querySelector('[aria-label="Minimize window"]').click(),{once:true}));
      await input0.press('Enter');
      await send(r1,1,'show running-config');
      if(minimized)await page.getByRole('button',{name:'Console R0',exact:true}).click();
      await waitComplete(r0,0,previous);
      if(!wire[0].text.includes('Ethernet0/0')||!wire[1].text.includes('Current configuration'))throw new Error('Expected real show output missing');
      const capture=await snapshot();
      if(capture.nodes[0].wsBytes<2000||capture.nodes[1].wsBytes<500)throw new Error('Output too small to validate large-command path');
      if(minimized&&capture.nodes[0].hiddenWriteCalls===0)throw new Error('Hidden output not observed');
      const nodes=Object.fromEntries(Object.entries(capture.nodes).map(([id,stats])=>{
        const {samples,...counters}=stats;return[id,{...counters,retainedWrites:samples.length,hiddenWrites:samples.filter(s=>!s.visible).length,newConnections:wire[id].opens,disconnections:wire[id].closes}];
      }));
      results.push({scenario:minimized?'R0 minimized during output, then restored':'two visible consoles',nodes});
    }
    if(errors.length)throw new Error(errors.join('\n'));
    await page.screenshot({path:'scratch/console-stability-after.png'});
    return{results,semantics:(await snapshot()).semantics,completion:'real output markers, fresh prompt, zero backlog; no physical-display timing claim'};
  }finally{page.off('websocket',onSocket);page.off('pageerror',onError);}
}
