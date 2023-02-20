package terraform

import (
	"log"

	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/configs"
	"github.com/hashicorp/terraform/internal/dag"
)

type checkTransformer struct {
	// Config for the entire module.
	Config *configs.Config

	// We only report the checks during the plan, as the apply operation
	// remembers checks from the plan stage.
	ReportChecks bool

	// There's no point running the checks if we aren't performing a plan or an
	// apply operation.
	ExecuteChecks bool
}

var _ GraphTransformer = (*checkTransformer)(nil)

func (t *checkTransformer) Transform(graph *Graph) error {
	return t.transform(graph, t.Config, graph.Vertices())
}

func (t *checkTransformer) transform(g *Graph, cfg *configs.Config, allNodes []dag.Vertex) error {
	moduleAddr := cfg.Path

	for _, check := range cfg.Module.Checks {
		configAddr := check.Addr().InModule(moduleAddr)

		log.Printf("[TRACE] checkTransformer: Nodes and edges for %s", configAddr)
		expand := &nodeExpandCheck{
			addr:   configAddr,
			config: check,
			makeInstance: func(addr addrs.AbsCheck, cfg *configs.Check) dag.Vertex {
				return &nodeCheckAssert{
					addr:          addr,
					config:        cfg,
					executeChecks: t.ExecuteChecks,
				}
			},
		}
		g.Add(expand)

		if t.ReportChecks && t.ExecuteChecks {
			report := &nodeReportCheck{
				addr: configAddr,
			}
			g.Add(report)

			for _, other := range allNodes {
				if resource, isResource := other.(GraphNodeConfigResource); isResource {
					resourceAddr := resource.ResourceAddr()
					if !resourceAddr.Module.Equal(moduleAddr) {
						// This resource isn't in the same module as our check
						// so skip it.
						continue
					}

					resourceCfg := cfg.Module.ResourceByAddr(resourceAddr.Resource)
					if resourceCfg != nil && resourceCfg.Container != nil && resourceCfg.Container.Accessible(check.Addr()) {
						// Make sure we report our checks before we execute any
						// embedded data resource.
						g.Connect(dag.BasicEdge(other, report))
						continue
					}
				}
			}
		}
	}

	for _, child := range cfg.Children {
		if err := t.transform(g, child, allNodes); err != nil {
			return err
		}
	}

	return nil
}
