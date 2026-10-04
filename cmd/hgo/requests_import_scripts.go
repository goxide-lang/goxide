package main

// ImportRequest describes an import without exposing the parser implementation.
// Bins preserves repeated name=path values without comma splitting.
type ImportRequest struct {
	Root, Out, GoTool, Lib string
	Plan, Offline          bool
	Bins                   []string
}

// ScriptsRequest requires an explicit action and project. Defaults are assigned
// by the adapter, never by an authorization service.
type ScriptsRequest struct {
	Root, Action, Subject, TargetDir, GoTool, Features string
	NoDefaultFeatures, Offline, Locked                 bool
}
