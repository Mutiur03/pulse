import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PulseClient } from '../src/client';
import { safeText } from '../src/privacy';

describe('input privacy', () => {
  let client: PulseClient;

  beforeEach(() => {
    localStorage.clear();
    globalThis.fetch = vi.fn(async () => new Response(null, { status: 202 })) as typeof fetch;
    document.body.innerHTML = `
      <form>
        <input id="email" name="email" type="email">
        <input id="pw" type="password">
        <textarea id="notes">secret notes</textarea>
        <label id="lbl">Card <input id="card" value="4111111111111111"></label>
        <div id="ce" contenteditable="true">typed secret</div>
        <button id="pay" type="button">Pay now</button>
      </form>`;
    client = new PulseClient({
      endpoint: 'http://ingest.test',
      projectId: '1',
      tracing: false,
      autocapture: { webVitals: false, pageviews: false, errors: false },
    });
    client.start();
  });
  afterEach(() => client.shutdown());

  const typed = ['hunter2', 'me@example.com', '4111111111111111', 'secret notes', 'typed secret'];

  it('never records what users type, only element metadata', () => {
    const push = vi.spyOn(client.events, 'push');
    (document.getElementById('email') as HTMLInputElement).value = 'me@example.com';
    (document.getElementById('pw') as HTMLInputElement).value = 'hunter2';
    for (const id of ['email', 'pw', 'notes', 'lbl', 'card', 'ce', 'pay']) {
      document.getElementById(id)!.click();
    }
    const clicks = push.mock.calls.map((c) => c[0]).filter((e) => e.event === '$autocapture');
    // 7 clicks, plus the browser forwarding the label click to its input.
    expect(clicks.length).toBe(8);
    const json = JSON.stringify(push.mock.calls.map((c) => c[0]));
    for (const secret of typed) expect(json).not.toContain(secret);

    expect(clicks[0].properties).toMatchObject({ $tag: 'input', $input_type: 'email', $input_name: 'email' });
    expect(clicks.at(-1)!.properties.$text).toBe('Pay now');
  });

  it('safeText skips nested form fields', () => {
    expect(safeText(document.getElementById('lbl')!)).toBe('Card');
  });
});
