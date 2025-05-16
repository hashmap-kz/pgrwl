package cmd

import "strings"

var confTempl = `
{
  "mode": {
    "name": "receive",
    "receive": {
      "listen_port": 7070,
      "directory": "/mnt/wal-archive",
      "slot": "bookstore_base_archiver",
      "no_loop": false
    },
    "serve": {
      "listen_port": 7070,
      "directory": "/mnt/wal-archive"
    }
  },
  "log": {
    "level": "info",
    "format": "text",
    "add_source": true
  },
  "storage": {
    "name": "s3",
    "compression": {
      "algo": "gzip"
    },
    "encryption": {
      "algo": "aes-256-gcm",
      "pass": "${PGRWL_ENCRYPT_PASS}"
    },
    "sftp": {
      "host": "sftp.example.com",
      "port": 22,
      "user": "${PGRWL_VM_USER}",
      "pass": "${PGRWL_VM_PASS}",
      "pkey_path": "/home/user/.ssh/id_rsa",
      "pkey_pass": "${PGRWL_SSH_PKEY_PASSPHRASE}"
    },
    "s3": {
      "url": "https://s3.example.com",
      "access_key_id": "AKIAEXAMPLE",
      "secret_access_key": "${PGRWL_SECRET_ACCESS_KEY}",
      "bucket": "postgres-backups",
      "region": "us-east-1",
      "use_path_style": true,
      "disable_ssl": false
    }
  }
}
`

var confTemplReceive = `
  {
    "mode": {
      "name": "receive",
      "receive": {
        "listen_port": 7070,
        "directory": "wals",
        "slot": "pgrwl_v5"
      }
    }
  }
`

var confTemplServe = `
  {
    "mode": {
      "name": "serve",
      "serve": {
        "listen_port": 7070,
        "directory": "wals"
      }
    }
  }
`

func GetConfigTemplateFull() string {
	return strings.TrimSpace(confTempl)
}

func GetConfigTemplateReceive() string {
	return strings.TrimSpace(confTemplReceive)
}

func GetConfigTemplateServe() string {
	return strings.TrimSpace(confTemplServe)
}
