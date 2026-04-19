package backupmode

import (
	"net/http"
)

func Init() http.Handler {
	return initHandlers()
}
