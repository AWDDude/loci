package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/AWDDude/loci/internal/model"
	"github.com/AWDDude/loci/internal/service"
)

func entityCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{Use: "entity", Short: "Find, create and edit entities"}
	cmd.AddCommand(
		entitySearchCmd(env), entityCreateCmd(env), entityGetCmd(env),
		entityUpdateCmd(env), entityDeleteCmd(env),
	)
	return cmd
}

func entitySearchCmd(env *Env) *cobra.Command {
	var typ string
	var limit int
	cmd := leaf("search <query>", "Find entities; the last row's uuid is the placeholder entity create needs", "entity_search", 1,
		func(cmd *cobra.Command, a []string) error {
			args := map[string]any{"query": a[0]}
			setIfChanged(cmd, args, "type", "type", typ)
			setIfChanged(cmd, args, "limit", "limit", limit)
			return call(cmd, env, "entity_search", args, printEntitySearch)
		})
	cmd.Flags().StringVar(&typ, "type", "", "only entities of this type")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum results (default 20)")
	return cmd
}

func printEntitySearch(w io.Writer, r service.EntitySearchResult) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "UUID\tTYPE\tNAME\tDESCRIPTION")
	for _, h := range r.Entities {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", h.Entity.ID, h.Entity.Type, nameWithAliases(h.Entity), h.Entity.Description)
	}
	fmt.Fprintf(tw, "%s\t(new)\tcreate a new entity\t\n", r.Placeholder)
	return tw.Flush()
}

func nameWithAliases(e model.Entity) string {
	if len(e.Aliases) == 0 {
		return e.Name
	}
	return fmt.Sprintf("%s (%s)", e.Name, strings.Join(e.Aliases, ", "))
}

func entityCreateCmd(env *Env) *cobra.Command {
	var name, description, typ string
	var aliases []string
	cmd := leaf("create <placeholder>", "Create an entity, given the placeholder from the latest entity search", "entity_create", 1,
		func(cmd *cobra.Command, a []string) error {
			args := map[string]any{"placeholder": a[0], "name": name, "description": description, "type": typ}
			setIfChanged(cmd, args, "aliases", "aliases", aliases)
			return call(cmd, env, "entity_create", args, printEntity)
		})
	cmd.Flags().StringVar(&name, "name", "", "display name (required)")
	cmd.Flags().StringVar(&description, "description", "", "one line that tells it apart from similar names (required)")
	cmd.Flags().StringVar(&typ, "type", "", "entity type (required): "+strings.Join(typeNames(model.EntityTypes), ", "))
	cmd.Flags().StringSliceVar(&aliases, "aliases", nil, "other names, comma-separated")
	for _, f := range []string{"name", "description", "type"} {
		_ = cmd.MarkFlagRequired(f)
	}
	return cmd
}

func printEntity(w io.Writer, e model.Entity) error {
	fmt.Fprintf(w, "%s (%s)  %s\n", e.Name, e.Type, e.ID)
	if len(e.Aliases) > 0 {
		fmt.Fprintf(w, "  aliases: %s\n", strings.Join(e.Aliases, ", "))
	}
	_, err := fmt.Fprintf(w, "  %s\n", e.Description)
	return err
}

func entityGetCmd(env *Env) *cobra.Command {
	return leaf("get <id>", "Show an entity with its edges and memory titles", "entity_get", 1,
		func(cmd *cobra.Command, a []string) error {
			return call(cmd, env, "entity_get", map[string]any{"id": a[0]}, printEntityDetail)
		})
}

func printEntityDetail(w io.Writer, d service.EntityDetail) error {
	if err := printEntity(w, d.Entity); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if len(d.Edges) > 0 {
		fmt.Fprintln(tw, "\nedges:")
		for _, e := range d.Edges {
			fmt.Fprintf(tw, "  %s\t%s (%s)\t%s\n", e.Name, e.Other.Name, e.Other.Type, e.Other.ID)
		}
	}
	if len(d.Memories) > 0 {
		fmt.Fprintln(tw, "\nmemories:")
		// Documentation order rather than map order, so output is stable.
		for _, lt := range model.LinkTypes {
			for _, m := range d.Memories[lt] {
				fmt.Fprintf(tw, "  %s\t%s\t%s\n", lt, m.Title, m.ID)
			}
		}
	}
	return tw.Flush()
}

func entityUpdateCmd(env *Env) *cobra.Command {
	var name, description, typ string
	var aliases []string
	cmd := leaf("update <id>", "Edit an entity; only the flags given change", "entity_update", 1,
		func(cmd *cobra.Command, a []string) error {
			args := map[string]any{"id": a[0]}
			setIfChanged(cmd, args, "name", "name", name)
			setIfChanged(cmd, args, "description", "description", description)
			setIfChanged(cmd, args, "type", "type", typ)
			if cmd.Flags().Changed("aliases") {
				if aliases == nil {
					aliases = []string{} // --aliases "" clears the list
				}
				args["aliases"] = aliases
			}
			return call(cmd, env, "entity_update", args, printEntity)
		})
	cmd.Flags().StringVar(&name, "name", "", "new display name (the old one is not kept as an alias)")
	cmd.Flags().StringVar(&description, "description", "", "new one-line description")
	cmd.Flags().StringVar(&typ, "type", "", "new entity type")
	cmd.Flags().StringSliceVar(&aliases, "aliases", nil, `replace all aliases, comma-separated ("" clears them)`)
	return cmd
}

func entityDeleteCmd(env *Env) *cobra.Command {
	return leaf("delete <id>", "Delete an entity with its edges and links", "entity_delete", 1,
		func(cmd *cobra.Command, a []string) error {
			return call(cmd, env, "entity_delete", map[string]any{"id": a[0]}, printDeleted("entity"))
		})
}

// deletedResult mirrors the delete tools' result.
type deletedResult struct {
	Deleted any `json:"deleted"`
}

func printDeleted(kind string) func(io.Writer, deletedResult) error {
	return func(w io.Writer, r deletedResult) error {
		switch d := r.Deleted.(type) {
		case string:
			_, err := fmt.Fprintf(w, "Deleted %s %s.\n", kind, d)
			return err
		case map[string]any:
			if kind == "edge" {
				_, err := fmt.Fprintf(w, "Deleted edge %v %v %v.\n", d["from"], d["type"], d["to"])
				return err
			}
			_, err := fmt.Fprintf(w, "Deleted link: memory %v %v entity %v.\n", d["memory"], d["type"], d["entity"])
			return err
		}
		_, err := fmt.Fprintf(w, "Deleted %s.\n", kind)
		return err
	}
}

func typeNames[T ~string](ts []T) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = string(t)
	}
	return out
}
