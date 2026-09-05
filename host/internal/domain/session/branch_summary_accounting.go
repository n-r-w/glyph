package session

import "errors"

// ValidateAccounting checks source identity, reported usage, and the persisted cost relationship.
func (summary BranchSummaryEntry) ValidateAccounting() error {
	if err := summary.Source.Validate(); err != nil {
		return err
	}
	if cost, present := summary.EstimatedCost.Get(); present {
		// A cost claim requires the reported tokens from an actual model source.
		modelSource, modelPresent := summary.Source.Model.Get()
		if !modelPresent || modelSource.Usage.IsNone() {
			return errors.New("branch summary cost requires reported model usage")
		}
		if !cost.Valid() {
			return errors.New("branch summary estimated cost is invalid")
		}
	}
	return nil
}
