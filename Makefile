.PHONY: up down logs rebuild tidy run test smoke clean

up:
	docker compose up --build -d
	@echo "API em http://localhost:8080"

down:
	docker compose down

clean:
	docker compose down -v

logs:
	docker compose logs -f events-service

rebuild:
	docker compose up --build -d --force-recreate events-service

# Roda fora do Docker, apontando para o Postgres publicado em 5433.
run:
	cd services/events && \
	DATABASE_URL="postgres://events:events@localhost:5433/eventsdb?sslmode=disable" \
	LOG_LEVEL=debug go run ./cmd/api

tidy:
	cd services/events && go mod tidy

test:
	cd services/events && go test ./...

smoke:
	./scripts/smoke.sh
