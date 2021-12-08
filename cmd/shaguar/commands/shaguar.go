package commands

import "github.com/spf13/cobra"

func ShaguarCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shag",
		Short: "Shaguar is a very fast car for ESP32",
	}

	cmd.AddCommand(ScanCmd(), PingCmd())
	return cmd
}
