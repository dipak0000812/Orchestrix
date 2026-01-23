.PHONY: help migrate-up migrate-down migrate-create db-reset

# Database connection
DB_URL := postgres://orchestrix:orchestrix_dev_password@localhost:5434/orchestrix_dev?sslmode=disable

help:
	@echo "Available commands:"
	@echo "  make migrate-up       - Apply all pending migrations"
	@echo "  make migrate-down     - Rollback last migration"
	@echo "  make migrate-create   - Create new migration (usage: make migrate-create name=add_priority)"
	@echo "  make db-reset         - Drop and recreate database (⚠️  destroys data)"

migrate-up:
	migrate -path migrations -database "$(DB_URL)" up

migrate-down:
	migrate -path migrations -database "$(DB_URL)" down 1

migrate-create:
	@if [ -z "$(name)" ]; then \
		echo "Error: name is required. Usage: make migrate-create name=add_priority"; \
		exit 1; \
	fi
	migrate create -ext sql -dir migrations -seq $(name)

db-reset:
	@echo "⚠️  WARNING: This will delete ALL data!"
	@read -p "Are you sure? [y/N] " -n 1 -r; \
	echo; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		migrate -path migrations -database "$(DB_URL)" down -all; \
		migrate -path migrations -database "$(DB_URL)" up; \
		echo "✓ Database reset complete"; \
	else \
		echo "Cancelled"; \
	fi