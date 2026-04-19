package backupmode

import (
	"net/http"

	"github.com/pgrwl/pgrwl/config"
)

func Init(cfg *config.Config) http.Handler {
	return initHandlers(cfg)
}
