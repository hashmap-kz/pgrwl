# Makefile for controlling PostgreSQL streaming replication mini-cluster

# Variables
COMPOSE = docker compose
COMPOSE_FILE = docker-compose.yml

# Targets

.PHONY: up down ps logs promote status primary standby clean restart

# Start the cluster
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

# Promote the standby
promote:
	$(COMPOSE) -f $(COMPOSE_FILE) exec pg-standby bash -c "su postgres -c 'pg_ctl promote -D /var/lib/postgresql/data'"
	@echo "🎉 Standby promoted to primary!"

# Show replication status on standby
status-standby:
	$(COMPOSE) -f $(COMPOSE_FILE) exec pg-standby psql -U postgres -p 5432 -c "SELECT timeline_id, pg_is_in_recovery(), pg_last_wal_receive_lsn() FROM pg_control_checkpoint(), pg_stat_wal_receiver;"

# Show replication status on primary
status-primary:
	$(COMPOSE) -f $(COMPOSE_FILE) exec pg-primary psql -U postgres -p 5432 -c "SELECT timeline_id, pg_current_wal_lsn() FROM pg_control_checkpoint();"

# Restart the cluster
restart:
	$(MAKE) down
	sleep 2
	$(MAKE) up

# Completely clean up volumes and containers
clean:
	$(COMPOSE) -f $(COMPOSE_FILE) down -v
	@echo "🧹 Containers and volumes are removed."

create-slot:
	$(COMPOSE) -f $(COMPOSE_FILE) exec pg-standby bash -c "su postgres -c 'pg_receivewal --no-password --no-loop --slot=pg_receivewal --create-slot --if-not-exists'"

switch-wal-primary:
	$(COMPOSE) -f $(COMPOSE_FILE) exec pg-primary psql -U postgres -p 5432 -c "SELECT pg_switch_wal();"

switch-wal-standby:
	$(COMPOSE) -f $(COMPOSE_FILE) exec pg-standby psql -U postgres -p 5432 -c "SELECT pg_switch_wal();"

# Default target
default: up
