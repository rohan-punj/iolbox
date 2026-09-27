// Run with Playwright CLI: run-code --filename app/tests/painter-prefix.playwright.js
// The CLI session must already be open on the deployed iolbox with a running lab.
async (page) => {
  const requests = [];
  const responses = new Map();
  const browserErrors = [];
  page.on('pageerror', error => browserErrors.push(error.message));
  page.on('websocket', socket => {
    if (!socket.url().includes('/control')) return;
    socket.on('framesent', ({ payload }) => {
      for (const line of String(payload).split('\n')) {
        try {
          const frame = JSON.parse(line);
          if (frame.op === 'painter.collect') requests.push(frame);
        } catch { /* Non-JSON frames are unrelated to painter requests. */ }
      }
    });
    socket.on('framereceived', ({ payload }) => {
      for (const line of String(payload).split('\n')) {
        try {
          const frame = JSON.parse(line);
          if (frame.id && typeof frame.ok === 'boolean') responses.set(frame.id, frame);
        } catch { /* Other control traffic is unrelated. */ }
      }
    });
  });
  const check = (condition, message) => { if (!condition) throw new Error(message); };
  await page.reload();
  // Wait for server state hydration; a transient Start button can disappear
  // while the running lab is restored after reload.
  await page.getByRole('button', { name: 'Stop lab', exact: true }).waitFor();
  const open = async () => {
    await page.getByRole('button', { name: 'Tools', exact: true }).click();
    const tools = page.getByRole('dialog', { name: 'Tools', exact: true });
    await tools.getByRole('button', { name: 'Topology painter', exact: true }).click();
    await tools.getByRole('button', { name: 'Close', exact: true }).click();
  };
  await open();
  const panel = page.getByRole('dialog', { name: 'Topology Painter', exact: true });
  const input = panel.getByRole('textbox', { name: 'Destination prefix or host', exact: true });
  const select = proto => panel.getByRole('radio', { name: proto, exact: true }).click();
  const raw = ' 203.0.113.0/24 ';
  const retained = async () => check(await input.inputValue() === raw, 'Destination was overwritten');
  await select('OSPF');
  await input.fill(raw);
  for (const proto of ['EIGRP', 'BGP', 'OSPF']) {
    await select(proto);
    await retained();
    check(await panel.getByRole('combobox').count() === 0, 'Routing node picker still present');
  }
  await select('STP');
  await panel.getByRole('combobox', { name: 'Pick the node to probe for VLANs' }).selectOption({ label: 'R1' });
  await select('OSPF');
  await retained();
  await panel.getByRole('button', { name: 'Close', exact: true }).click();
  await open();
  await retained();
  const paint = async (proto, expected) => {
    const before = requests.length;
    await panel.getByRole('button', { name: /^(Paint|Re-paint)$/ }).click();
    await page.waitForFunction(() => !Array.from(document.querySelectorAll('.pp-run')).some(el => el.textContent.includes('Painting')), null, { timeout: 90000 });
    check(requests.length === before + 1, 'Expected one painter request');
    const request = requests.at(-1);
    check(request.args.proto === proto.toLowerCase(), 'Wrong protocol sent');
    check(request.args.dest === expected, `Wrong destination sent: ${request.args.dest}`);
    const response = responses.get(request.id);
    check(response?.ok === true, 'Painter did not return a successful snapshot');
    check(response.result.proto === proto.toLowerCase(), 'Wrong snapshot protocol');
    check(response.result.dest === expected, 'Snapshot destination differs from the input');
  };
  for (const proto of ['OSPF', 'EIGRP', 'BGP']) {
    await select(proto);
    await paint(proto, raw.trim());
    await retained();
  }
  await paint('BGP', raw.trim());
  await retained();
  await panel.getByRole('button', { name: 'Clear', exact: true }).click();
  await retained();
  for (const proto of ['EIGRP', 'BGP']) {
    await select(proto);
    await input.fill('');
    const before = requests.length;
    await panel.getByRole('button', { name: 'Paint', exact: true }).click();
    check(requests.length === before, 'Empty required destination sent');
    check((await panel.innerText()).includes('destination'), 'Missing required destination error');
  }
  await select('OSPF');
  await paint('OSPF', undefined);
  check(await input.inputValue() === '', 'Optional blank destination changed');
  await select('BGP');
  await input.fill('203.0.113.7');
  await paint('BGP', '203.0.113.7');
  check(await input.inputValue() === '203.0.113.7', 'Host destination changed');
  await page.screenshot({ path: 'scratch/painter-prefix-after.png' });
  check(browserErrors.length === 0, `Browser runtime errors: ${browserErrors.join('; ')}`);
  return { passed: true, requests: requests.map(({args}) => args), successfulSnapshots: requests.length, browserErrors, screenshot: 'scratch/painter-prefix-after.png' };
}
