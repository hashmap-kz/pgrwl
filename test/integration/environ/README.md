### Login into container (ssh)

```
chmod 0600 files/dotfiles/.ssh/id_ed25519
ssh postgres@localhost -p 2323 -i files/dotfiles/.ssh/id_ed25519
```

### Login into container (sh)

```
docker exec -it pg17 bash
```

### Run all tests

```
docker exec -it pg17 chmod +x /var/lib/postgresql/scripts/runners/run-tests.sh
docker exec -it pg17 su - postgres -c /var/lib/postgresql/scripts/runners/run-tests.sh
```

### Login into "VM" (an SFTP storage)

```
chmod 0600 files/dotfiles/.ssh/id_ed25519
ssh testuser@localhost -p 2325 -i files/dotfiles/.ssh/id_ed25519
```
