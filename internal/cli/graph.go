package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/AWDDude/loci/internal/model"
)

func edgeCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{Use: "edge", Short: "Relate entities to each other"}
	cmd.AddCommand(
		leaf("create <from> <type> <to>", "Relate two entities; inverse names like child_of are accepted", "edge_create", 3,
			func(cmd *cobra.Command, a []string) error {
				return call(cmd, env, "edge_create", edgeArgs(a), printEdge)
			}),
		leaf("delete <from> <type> <to>", "Remove an edge, given in either direction", "edge_delete", 3,
			func(cmd *cobra.Command, a []string) error {
				return call(cmd, env, "edge_delete", edgeArgs(a), printDeleted("edge"))
			}),
	)
	return cmd
}

func edgeArgs(a []string) map[string]any {
	return map[string]any{"from": a[0], "type": a[1], "to": a[2]}
}

func printEdge(w io.Writer, e model.Edge) error {
	_, err := fmt.Fprintf(w, "%s %s %s\n", e.From, e.Type, e.To)
	return err
}

func linkCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{Use: "link", Short: "Link memories to entities"}
	cmd.AddCommand(
		leaf("create <memory> <type> <entity>", "Link a memory to another entity", "link_create", 3,
			func(cmd *cobra.Command, a []string) error {
				return call(cmd, env, "link_create", linkArgs(a), printLink)
			}),
		leaf("delete <memory> <type> <entity>", "Remove a link; refused for a memory's last link", "link_delete", 3,
			func(cmd *cobra.Command, a []string) error {
				return call(cmd, env, "link_delete", linkArgs(a), printDeleted("link"))
			}),
	)
	return cmd
}

func linkArgs(a []string) map[string]any {
	return map[string]any{"memory": a[0], "type": a[1], "entity": a[2]}
}

func printLink(w io.Writer, l model.Link) error {
	_, err := fmt.Fprintf(w, "Linked memory %s to entity %s as %s.\n", l.MemoryID, l.EntityID, l.Type)
	return err
}
