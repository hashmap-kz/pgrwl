package backupmode

import (
	"github.com/pgrwl/pgrwl/config"
	"net/http"
)

func Init(cfg *config.Config) http.Handler {
	return initHandlers(cfg)
}
