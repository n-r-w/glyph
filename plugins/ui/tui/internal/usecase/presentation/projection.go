package presentation

// applyProjection changes private projection state and invalidates its detached display body together.
func (model *interaction) applyProjection(update event) {
	model.state = model.state.Apply(update)
	model.projectionChanged = true
}
