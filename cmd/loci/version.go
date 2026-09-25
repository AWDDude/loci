package main

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

type versionInfo struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Go      string `json:"go"`
}

func newVersionCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := versionInfo{
				Version: version,
				OS:      runtime.GOOS,
				Arch:    runtime.GOARCH,
				Go:      runtime.Version(),
			}
			out := cmd.OutOrStdout()
			if flags.json {
				return json.NewEncoder(out).Encode(info)
			}
			_, err := fmt.Fprintf(out, "loci %s (%s/%s, %s)\n", info.Version, info.OS, info.Arch, info.Go)
			return err
		},
	}
}
