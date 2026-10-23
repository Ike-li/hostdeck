// getJSON reads the console's API; postJSON writes to it. The API answers
// with error codes, which the page words from its dictionaries; postJSON
// throws the backend's error object so every caller can word it the same way.
export async function getJSON(url) {
  const resp = await fetch(url, { cache: 'no-store' });
  if (!resp.ok) throw new Error(`${url}: HTTP ${resp.status}`);
  return resp.json();
}

export async function postJSON(url, body) {
  const init = { method: 'POST' };
  if (body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' };
    init.body = JSON.stringify(body);
  }
  const resp = await fetch(url, init);
  const parsed = await resp.json().catch(() => ({}));
  if (!resp.ok) throw parsed.error || { code: '', detail: `HTTP ${resp.status}` };
  return parsed;
}
