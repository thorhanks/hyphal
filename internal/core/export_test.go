package core

// SetBeforeInvert lets tests write from another connection between planning an
// inverse and committing it.
func (o *Outline) SetBeforeInvert(f func()) {
	o.beforeInvert = f
}
