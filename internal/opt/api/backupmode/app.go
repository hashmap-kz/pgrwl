package backupmode

import (
	"context"
)

type Opts struct {
	Gate      Gate
	Directory string
	AppCtx    context.Context
}
