package compile

// Entry is the selected package relative to GoRoot.
func (p *Project) Entry() string { return p.stages[p.plans.Target].entry }
