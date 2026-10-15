// Lista, formulario e acoes (publicar/cancelar/editar/excluir) de eventos.
let editingId = null;
const labels = { draft: "rascunho", published: "publicado", cancelled: "cancelado" };

async function load() {
  const q = new URLSearchParams({ limit: 50 });
  if ($("fCity").value.trim()) q.set("city", $("fCity").value.trim());
  if ($("fStatus").value) q.set("status", $("fStatus").value);
  try {
    const page = await api("/events?" + q);
    $("total").textContent = "(" + page.total + ")";
    $("list").innerHTML = page.items.length ? page.items.map(card).join("") : '<div class="empty">Nenhum evento encontrado.</div>';
    window._items = Object.fromEntries(page.items.map((e) => [e.id, e]));
  } catch (e) { toast(e.message, true); }
}

function card(e) {
  return `<div class="event">
    <h3>${esc(e.name)} <span class="badge ${e.status}">${labels[e.status]}</span></h3>
    <div class="meta">📍 ${esc(e.venue)} — ${esc(e.city)} · 🗓️ ${date(e.starts_at)} · 👥 ${e.capacity} · ${money(e.price_cents)}</div>
    ${e.description ? `<div class="meta">${esc(e.description)}</div>` : ""}
    <div class="actions">
      ${e.status === "draft" ? `<button class="small ok" onclick="act('${e.id}','publish')">Publicar</button>` : ""}
      ${e.status !== "cancelled" ? `<button class="small warn" onclick="act('${e.id}','cancel')">Cancelar</button>` : ""}
      <button class="small ghost" onclick="edit('${e.id}')">Editar</button>
      <button class="small bad" onclick="del('${e.id}')">Excluir</button>
    </div></div>`;
}

async function act(id, what) {
  try { await api(`/events/${id}/${what}`, { method: "POST" }); toast(what === "publish" ? "Evento publicado" : "Evento cancelado"); load(); }
  catch (e) { toast(e.message, true); }
}

async function del(id) {
  if (!confirm("Excluir este evento?")) return;
  try { await api("/events/" + id, { method: "DELETE" }); toast("Evento excluído"); load(); }
  catch (e) { toast(e.message, true); }
}

function toLocalInput(iso) {
  const d = new Date(iso), p = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth()+1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

function edit(id) {
  const e = window._items[id], f = $("form");
  editingId = id;
  f.name.value = e.name; f.description.value = e.description; f.venue.value = e.venue; f.city.value = e.city;
  f.starts_at.value = toLocalInput(e.starts_at); f.capacity.value = e.capacity; f.price.value = e.price_cents / 100;
  $("formTitle").textContent = "Editar evento"; $("btnSave").textContent = "Salvar";
  f.scrollIntoView({ behavior: "smooth" });
}

function resetForm() {
  editingId = null; $("form").reset();
  $("formTitle").textContent = "Novo evento"; $("btnSave").textContent = "Criar";
}

async function saveEvent(ev) {
  ev.preventDefault();
  const f = ev.target;
  const body = {
    name: f.name.value, description: f.description.value, venue: f.venue.value, city: f.city.value,
    starts_at: new Date(f.starts_at.value).toISOString(),
    capacity: parseInt(f.capacity.value, 10),
    price_cents: Math.round(parseFloat(f.price.value || "0") * 100),
  };
  try {
    await api(editingId ? "/events/" + editingId : "/events", {
      method: editingId ? "PUT" : "POST",
      headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
    });
    toast(editingId ? "Evento atualizado" : "Evento criado");
    resetForm(); load();
  } catch (e) { toast(e.message, true); }
}
