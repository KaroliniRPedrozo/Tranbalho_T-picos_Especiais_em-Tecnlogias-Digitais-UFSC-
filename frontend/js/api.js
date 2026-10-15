// Camada de acesso a API e utilitarios compartilhados pelas outras telas.
const API = "/api/v1";
const $ = (id) => document.getElementById(id);

function toast(msg, err) {
  const t = $("toast");
  t.textContent = msg; t.className = "show" + (err ? " err" : "");
  clearTimeout(toast.t); toast.t = setTimeout(() => t.className = "", 3500);
}

// Chama a API; erro HTTP vira Error com a mensagem (e campos) do envelope.
async function api(path, opts) {
  const res = await fetch(API + path, opts);
  if (res.status === 204) return null;
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    const e = body.error || {};
    const fields = e.fields ? " (" + Object.entries(e.fields).map(([k, v]) => k + ": " + v).join("; ") + ")" : "";
    throw new Error((e.message || "erro " + res.status) + fields);
  }
  return body;
}

const money = (c) => (c / 100).toLocaleString("pt-BR", { style: "currency", currency: "BRL" });
const date = (s) => new Date(s).toLocaleString("pt-BR", { dateStyle: "short", timeStyle: "short" });
const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ "&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;" }[c]));
