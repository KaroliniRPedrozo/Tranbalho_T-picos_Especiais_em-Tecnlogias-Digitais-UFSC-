// Ponto de entrada: liga os botoes as funcoes e carrega a lista inicial.
$("form").addEventListener("submit", saveEvent);
$("btnReset").onclick = resetForm;
$("btnFilter").onclick = load;
$("btnCep").onclick = searchCep;

load();
