COMPOSE = docker compose
COMPOSE_FILE = docker-compose.yml

# Targets

.PHONY: build up down ps logs clean restart basebackup gendata teardown restore

# Build images, up containers
build:
	@rm -rf bin
	@sh -c 'cd ../ && make build && mv bin examples'
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

# Completely clean up volumes and containers
clean:
	@rm -rf bin
	$(COMPOSE) -f $(COMPOSE_FILE) down -v
	@echo "🧹 Containers and volumes are removed."

# Run pg_basebackup
basebackup:
	@docker exec -it pg-primary chmod +x /var/lib/postgresql/scripts/tests/01-basebackup.sh
	$(COMPOSE) -f $(COMPOSE_FILE) exec pg-primary bash -c "su postgres -c '/var/lib/postgresql/scripts/tests/01-basebackup.sh'"
	@echo "🎉 Basebackup created!"

# Gen data
gendata:
	@docker exec -it pg-primary chmod +x /var/lib/postgresql/scripts/tests/02-gendata.sh
	$(COMPOSE) -f $(COMPOSE_FILE) exec pg-primary bash -c "su postgres -c '/var/lib/postgresql/scripts/tests/02-gendata.sh'"
	@echo "🎉 Cluster populated with data!"

# Teardown cluster
teardown:
	@docker exec -it pg-primary chmod +x /var/lib/postgresql/scripts/tests/04-teardown.sh
	$(COMPOSE) -f $(COMPOSE_FILE) exec pg-primary bash -c "su postgres -c '/var/lib/postgresql/scripts/tests/04-teardown.sh'"
	@echo "🎉 Cluster destroyed!"

# Restore from basebackup and the wal-archive
restore:
	@docker exec -it pg-primary chown -R postgres:postgres /mnt/wal-archive
	@docker exec -it pg-primary chmod +x /var/lib/postgresql/scripts/tests/05-restore.sh
	$(COMPOSE) -f $(COMPOSE_FILE) exec pg-primary bash -c "su postgres -c '/var/lib/postgresql/scripts/tests/05-restore.sh'"
	@echo "🎉 Cluster restored!"


# Default target
default: up
