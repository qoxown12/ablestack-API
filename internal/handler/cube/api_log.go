package cube

import (
	"ablecloud.io/ablestack-api/internal/infra/logging"
)

func appendAbleStackAPILog(component string, format string, args ...any) {
	logging.AppendActionLog(component, format, args...)
}
