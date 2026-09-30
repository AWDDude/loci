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

func memoryCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{Use: "memory", Short: "Find, create and edit memories"}
	cmd.AddCommand(
		memorySearchCmd(env), memoryCreateCmd(env), memoryGetCmd(env),
		memoryUpdateCmd(env), memoryDeleteCmd(env), memoryListCmd(env),
	)
	return cmd
}

func memorySearchCmd(env *Env) *cobra.Command {
	var limit int
	cmd := leaf("search <query>", "Keyword search over memories; the last row's uuid is the placeholder memory create needs", "memory_search", 1,
		func(cmd *cobra.Command, a []string) error {
			args := map[string]any{"query": a[0]}
			setIfChanged(cmd, args, "limit", "limit", limit)
			return call(cmd, env, "memory_search", args, printMemorySearch)
		})
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum results (default 20)")
	return cmd
}

func printMemorySearch(w io.Writer, r service.MemorySearchResult) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "UUID\tTITLE")
	for _, h := range r.Memories {
		fmt.Fprintf(tw, "%s\t%s\n", h.Memory.ID, h.Memory.Title)
	}
	fmt.Fprintf(tw, "%s\t(new) create a new memory\n", r.Placeholder)
	return tw.Flush()
}

func memoryCreateCmd(env *Env) *cobra.Command {
	var title, content string
	var links []string
	cmd := leaf("create <placeholder>", "Create a memory linked to at least one entity, given the placeholder from the latest memory search", "memory_create", 1,
		func(cmd *cobra.Command, a []string) error {
			specs, err := parseLinks(links)
			if err != nil {
				return err
			}
			body, err := readContent(cmd, content)
			if err != nil {
				return err
			}
			args := map[string]any{"placeholder": a[0], "title": title, "content": body, "links": specs}
			return call(cmd, env, "memory_create", args, printMemoryDetail)
		})
	cmd.Flags().StringVar(&title, "title", "", "one-line summary (required)")
	cmd.Flags().StringVar(&content, "content", "", `the memory itself, or "-" to read it from stdin (required)`)
	cmd.Flags().StringArrayVar(&links, "link", nil, "TYPE:ENTITY_UUID, repeatable, at least one (required); types: "+strings.Join(typeNames(model.LinkTypes), ", "))
	for _, f := range []string{"title", "content", "link"} {
		_ = cmd.MarkFlagRequired(f)
	}
	return cmd
}

// parseLinks turns --link TYPE:ENTITY values into memory_create's links.
// Validating the type is left to the service, like every other rule.
func parseLinks(values []string) ([]map[string]any, error) {
	out := make([]map[string]any, len(values))
	for i, v := range values {
		typ, entity, ok := strings.Cut(v, ":")
		if !ok {
			return nil, fmt.Errorf("--link %q: want TYPE:ENTITY_UUID", v)
		}
		out[i] = map[string]any{"type": typ, "entity": entity}
	}
	return out, nil
}

func printMemory(w io.Writer, m model.Memory) error {
	fmt.Fprintf(w, "%s  %s\n", m.Title, m.ID)
	_, err := fmt.Fprintf(w, "\n%s\n", m.Content)
	return err
}

func printMemoryDetail(w io.Writer, d service.MemoryDetail) error {
	fmt.Fprintf(w, "%s  %s\n", d.Memory.Title, d.Memory.ID)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, l := range d.Links {
		fmt.Fprintf(tw, "  %s\t%s (%s)\t%s\n", l.Type, l.Entity.Name, l.Entity.Type, l.Entity.ID)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "\n%s\n", d.Memory.Content)
	return err
}

func memoryGetCmd(env *Env) *cobra.Command {
	return leaf("get <id>", "Show a memory with its links", "memory_get", 1,
		func(cmd *cobra.Command, a []string) error {
			return call(cmd, env, "memory_get", map[string]any{"id": a[0]}, printMemoryDetail)
		})
}

func memoryUpdateCmd(env *Env) *cobra.Command {
	var title, content string
	cmd := leaf("update <id>", "Edit a memory's title or content; only the flags given change", "memory_update", 1,
		func(cmd *cobra.Command, a []string) error {
			args := map[string]any{"id": a[0]}
			setIfChanged(cmd, args, "title", "title", title)
			if cmd.Flags().Changed("content") {
				body, err := readContent(cmd, content)
				if err != nil {
					return err
				}
				args["content"] = body
			}
			return call(cmd, env, "memory_update", args, printMemory)
		})
	cmd.Flags().StringVar(&title, "title", "", "new one-line summary")
	cmd.Flags().StringVar(&content, "content", "", `new content, or "-" to read it from stdin`)
	return cmd
}

func memoryDeleteCmd(env *Env) *cobra.Command {
	return leaf("delete <id>", "Delete a memory and its links", "memory_delete", 1,
		func(cmd *cobra.Command, a []string) error {
			return call(cmd, env, "memory_delete", map[string]any{"id": a[0]}, printDeleted("memory"))
		})
}

func memoryListCmd(env *Env) *cobra.Command {
	var linkType string
	cmd := leaf("list <entity>", "List the memories linked to an entity, newest first", "memory_list", 1,
		func(cmd *cobra.Command, a []string) error {
			args := map[string]any{"entity": a[0]}
			setIfChanged(cmd, args, "link-type", "link_type", linkType)
			return call(cmd, env, "memory_list", args, printMemoryList)
		})
	cmd.Flags().StringVar(&linkType, "link-type", "", "only memories linked by this type")
	return cmd
}

func printMemoryList(w io.Writer, r service.MemoryListResult) error {
	for i, lm := range r.Memories {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "[%s] %s  %s\n", lm.LinkType, lm.Memory.Title, lm.Memory.ID)
		fmt.Fprintf(w, "  %s\n", strings.ReplaceAll(lm.Memory.Content, "\n", "\n  "))
	}
	if len(r.Memories) == 0 {
		_, err := fmt.Fprintln(w, "No memories.")
		return err
	}
	return nil
}
