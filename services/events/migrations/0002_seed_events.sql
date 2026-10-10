-- Seed apenas para facilitar a demonstracao em aula.
INSERT INTO events (name, description, venue, city, starts_at, capacity, price_cents, status)
SELECT
    'Festival de Verao 2027',
    'Line-up com bandas regionais no litoral sul.',
    'Arena Mar Grosso',
    'Ararangua',
    now() + interval '90 days',
    5000,
    12000,
    'published'
WHERE NOT EXISTS (SELECT 1 FROM events);

INSERT INTO events (name, description, venue, city, starts_at, capacity, price_cents, status)
SELECT
    'Workshop de Microsservicos em Go',
    'Dia inteiro de pratica com gRPC, Docker e observabilidade.',
    'Bloco de Informatica',
    'Criciuma',
    now() + interval '30 days',
    80,
    0,
    'draft'
WHERE (SELECT count(*) FROM events) < 2;
