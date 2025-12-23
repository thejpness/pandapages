.PHONY: up down logs psql migrate

up:
	docker compose -f docker-compose.dev.yml up -d --build

down:
	docker compose -f docker-compose.dev.yml down

logs:
	docker compose -f docker-compose.dev.yml logs -f --tail=200

migrate:
	docker compose -f docker-compose.dev.yml run --rm migrate

psql:
	docker compose -f docker-compose.dev.yml exec postgres psql -U pandapages -d pandapages
