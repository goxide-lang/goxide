package compile

import (
	"context"
	"errors"
	"os"

	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/project"
)

// publishMacroWork shares only successfully compiled intermediate work. Each
// Prepare still generates and invokes Go for its current inputs; this slot is
// never used as an existence-based freshness check or as a live importer root.
func (p *Project) publishMacroWork(ctx context.Context) error {
	type work struct {
		stage *stagedRole
		spec  project.UnitSpec
	}
	var stages []work
	for _, graph := range p.graphs {
		if stage := p.stages[graph]; stage != nil && len(stage.wasm) != 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			if stage.spec == nil {
				spec, err := unitSpec(stage.graph, stage.target, stage.kind, stage.config, stage.features, stage.sugar, nil, false, stage.modules[0].StableGo)
				if err != nil {
					return err
				}
				spec.Config = project.Digest([]string{spec.Config, stage.sdk, procmacro.ProfileID})
				stage.spec = &spec
			}
			stages = append(stages, work{stage, *stage.spec})
		}
	}
	if len(stages) == 0 {
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root := p.plans.Target.Root
	if p.options.Editor {
		// Existing editor lanes have independent leases; creating configuration
		// units remains the CLI's responsibility. Private work is always usable.
		for _, work := range stages {
			if err := project.PublishEditorMacroWork(ctx, root.Root, root.Module, p.sourceLayoutRoot, work.spec, work.stage.dir); err != nil {
				return err
			}
		}
		return nil
	}
	release, err := project.LockProject(root.Root)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			// Another publisher can finish; private results remain usable.
			return ctx.Err()
		}
		return err
	}
	defer release()
	layout, err := project.OpenLayout(root.Root, root.Module, p.layoutRoot)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return ctx.Err()
		}
		return err
	}
	defer layout.Close()
	for _, work := range stages {
		if err := ctx.Err(); err != nil {
			return err
		}
		unit, err := layout.Begin(work.spec)
		if err != nil {
			return err
		}
		if err := unit.PublishMacroWork(ctx, work.stage.dir, p.options.Editor); err != nil {
			return err
		}
	}
	return nil
}
