// Consulta de CEP (API externa ViaCEP, via backend).
async function searchCep() {
  const cep = $("cep").value.replace(/\D/g, "");
  try {
    const a = await api("/addresses/" + cep);
    $("cepOut").textContent = `${a.street}\n${a.neighborhood} — ${a.city}/${a.state}\nCEP ${a.cep}`;
  } catch (e) { $("cepOut").textContent = ""; toast(e.message, true); }
}
