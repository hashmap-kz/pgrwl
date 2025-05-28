COMPOSE = docker compose
COMPOSE_FILE = docker-compose.yml

# Targets

.PHONY: build up down ps logs restart

# Build images, up containers
build:
	$(COMPOSE) -f $(COMPOSE_FILE) up -d --build
	@echo "✅ Cluster is starting..."

# Up containers
up:
	$(COMPOSE) -f $(COMPOSE_FILE) up -d
	@echo "✅ Cluster is starting..."

# Stop the cluster
down:
	$(COMPOSE) -f $(COMPOSE_FILE) down -v
	@echo "🛑 Cluster is stopped."

# Show running containers
ps:
	$(COMPOSE) -f $(COMPOSE_FILE) ps

# Show logs (follow)
logs:
	$(COMPOSE) -f $(COMPOSE_FILE) logs -f

# Restart the cluster
restart:
	$(MAKE) down
	sleep 2
	$(MAKE) build

switch-wal:
	$(COMPOSE) -f $(COMPOSE_FILE) exec pg-primary psql -U postgres -p 5432 -c "drop table if exists xxx; select pg_switch_wal(); create table if not exists xxx();"
