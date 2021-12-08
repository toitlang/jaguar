package commands

import "github.com/spf13/cobra"

func JagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jag",
		Short: "Jaguar is a very fast car for ESP32",
	}

	cmd.AddCommand(ScanCmd(), PingCmd(), RunCmd())
	return cmd
}
