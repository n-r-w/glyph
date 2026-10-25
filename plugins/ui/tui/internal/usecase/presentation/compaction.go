package presentation

import "github.com/samber/mo"

// applyCompactionEntry records a saved identity and projects its summary without inventing tree topology.
func (model *interaction) applyCompactionEntry(entry TreeEntry) {
	if _, applied := model.compactionEntries[entry.ID]; applied {
		return
	}
	if model.compactionEntries == nil {
		model.compactionEntries = make(map[string]struct{})
	}
	model.compactionEntries[entry.ID] = struct{}{}
	model.state.Transcript = append(model.state.Transcript, NewTextLine(LineCompaction, mo.Some(entry.Text)))
	model.projectionChanged = true
}
