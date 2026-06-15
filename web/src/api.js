const API = "/api";

export async function api(path, opts) {
  const res = await fetch(`${API}${path}`, { ...opts, credentials: "same-origin" });
  const text = await res.text();
  let data;
  try {
    data = text ? JSON.parse(text) : {};
  } catch {
    data = { ok: false, error: { message: `Unexpected response from ${path}` } };
  }
  data.status = res.status;
  return data;
}


export { API };
