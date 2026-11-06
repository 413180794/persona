package assistant

import "errors"

var errInvalidQuestionCancel = errors.New("cancellation requires a supported question and no answer or other action")
