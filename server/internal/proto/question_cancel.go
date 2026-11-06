package proto

import "reflect"

// IsQuestionCancel rejects mixed resolutions so cancellation can never carry an
// answer, approval, terminal input, or another action along with it.
func (r Resolution) IsQuestionCancel() bool {
	if !r.Cancel {
		return false
	}
	r.Cancel = false
	return reflect.ValueOf(r).IsZero()
}
